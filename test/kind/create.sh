#!/usr/bin/env bash

# The routines in this file sets up a kind (kubernetes in docker) based
# topology with a controller, testa gateway cluster and testc gateway cluster
# The script does the necessary things to ensure that the clusters are
# connected to each other and the controller is programmed with a sample
# user(agent) and app(connector), the Agent connecting to testa and connector
# connecting to testc cluster

tmpdir=/tmp/nextensio-kind
kubectl=$tmpdir/kubectl
istioctl=$tmpdir/istioctl
helm=$tmpdir/linux-amd64/helm

function download_images {
    docker pull registry.gitlab.com/nextensio/ux/ux-deploy:latest
    docker pull registry.gitlab.com/nextensio/controller/controller-test:latest
    docker pull registry.gitlab.com/nextensio/cluster/minion:latest
    docker pull registry.gitlab.com/nextensio/clustermgr/mel-deploy:latest
    docker pull registry.gitlab.com/nextensio/agent/agent-deploy:latest
}

# Create a controller
function create_controller {
    kind create cluster --config ./kind-config.yaml --name controller

    kind load docker-image registry.gitlab.com/nextensio/ux/ux-deploy:latest --name controller
    kind load docker-image registry.gitlab.com/nextensio/controller/controller-test:latest --name controller

    # metallb as a loadbalancer to map services to externally accessible IPs
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/namespace.yaml
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/metallb.yaml
    $kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"
    # haproxy as an "ingress" mechanism - we dont use istio here, thats an overkill
    $kubectl apply -f https://raw.githubusercontent.com/haproxytech/kubernetes-ingress/master/deploy/haproxy-ingress.yaml
    # hostpath-provisioner for mongodb pods to get persistent storage from kubernetes host disk
    $kubectl delete storageclass standard
    $helm repo add rimusz https://charts.rimusz.net
    $helm repo update
    $helm upgrade --install hostpath-provisioner --namespace kube-system rimusz/hostpath-provisioner
}

function bootstrap_controller {
    my_ip=$1

    $kubectl config use-context kind-controller

    tmpf=$tmpdir/controller.yaml
    cp controller.yaml $tmpf
    sed -i "s/REPLACE_SELF_NODE_IP/$my_ip/g" $tmpf
    $kubectl apply -f $tmpf
    $kubectl apply -f mongo.yaml
}

# Create kind clusters for testa and testc
function create_cluster {
    cluster=$1

    # Create a docker-in-docker kubernetes cluster with a single node (control-plane) running everything
    kind create cluster --config ./kind-config.yaml --name $cluster

    # This is NOT the right thing to do in real deployment, either we should limit the 
    # roles (RBAC) of the clustermgr or even better make clustermgr use kube APIs instead
    # of kubectl
    $kubectl create clusterrolebinding permissive-binding \
        --clusterrole=cluster-admin \
        --user=admin \
        --user=kubelet \
        --group=system:serviceaccounts

    # Install istio. This is nothing but the demo.yaml in the istio bundle, with addonComponents
    # prometheus, kiali, grafana, tracing all set to false. 
    $istioctl manifest apply -f ./istio.yaml

    # Install metallb. metallb exposes services inside the cluster via external IP addresses
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/namespace.yaml
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/metallb.yaml
    $kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"

    # kind needs all images locally present, it wont download from any registry
    kind load docker-image registry.gitlab.com/nextensio/cluster/minion:latest --name $cluster
    kind load docker-image registry.gitlab.com/nextensio/clustermgr/mel-deploy:latest --name $cluster

    # Create ssl keys/certificates for agents/connectors to establish secure websocket
    openssl req -out $tmpdir/$cluster-gw.csr -newkey rsa:2048 -nodes -keyout $tmpdir/$cluster-gw.key \
        -subj "/CN=gateway.$cluster.nextensio.net/O=Nextensio Gateway $cluster"
    openssl x509 -req -days 365 -CA $tmpdir/rootca.crt -CAkey $tmpdir/rootca.key -set_serial 0 -in $tmpdir/$cluster-gw.csr -out $tmpdir/$cluster-gw.crt
}

function bootstrap_cluster {
    cluster=$1
    my_ip=$2
    ctrl_ip=$3

    $kubectl config use-context kind-$cluster

    $kubectl create -n istio-system secret tls gw-credential --key=$tmpdir/$cluster-gw.key --cert=$tmpdir/$cluster-gw.crt

    # Deploy the cluster manager "mel"
    tmpf=$tmpdir/$cluster-mel.yaml
    cp mel.yaml $tmpf
    sed -i "s/REPLACE_CLUSTER/$cluster/g" $tmpf
    sed -i "s/REPLACE_SELF_NODE_IP/$my_ip/g" $tmpf
    sed -i "s/REPLACE_CONTROLLER_IP/$ctrl_ip/g" $tmpf
    $kubectl apply -f $tmpf

    # Install loadbalancer to attract traffic to istio ingress gateway via external IP (docker contaier IP)
    tmpf=$tmpdir/$cluster-metallb.yaml
    cp metallb.yaml $tmpf
    sed -i "s/REPLACE_SELF_NODE_IP/$my_ip/g" $tmpf
    $kubectl apply -f $tmpf

    # Find consul dns server address. Mel would have launched consul pods, so wait
    # for the service to be available
    consul_dns=`$kubectl get svc $cluster-consul-dns -n consul-system -o jsonpath='{.spec.clusterIP}'`
    while [ -z "$consul_dns" ];
    do
      consul_dns=`$kubectl get svc $cluster-consul-dns -n consul-system -o jsonpath='{.spec.clusterIP}'`
      echo "waiting for consul, sleeping 5 seconds"
      sleep 5
    done
    echo "Success from server: service $cluster-consul-dns found"

    # Point dns server to redirect to consul for lookups of x.y.consul names
    tmpf=$tmpdir/$cluster-coredns.yaml
    # $tmpdir/coredns.yaml has been created before this is called
    cp $tmpdir/coredns.yaml $tmpf
    sed -i "s/REPLACE_CONSUL_DNS/$consul_dns/g" $tmpf
    sed -i "s/REPLACE_CONTROLLER_IP/$ctrl_ip/g" $tmpf
    $kubectl replace -n kube-system -f $tmpf
}

# Setup prepared query so that consul forwards the dns lookup to multiple DCs
# TODO: What if consul pod crashes, do we have to reapply these rules or consul saves it ?
function consul_query_config {
    cluster=$1

    $kubectl config use-context kind-$cluster
    $kubectl exec -it $cluster-consul-server-0 -n consul-system -- curl --request POST http://127.0.0.1:8500/v1/query --data-binary @- << EOF
{
  "Name": "",
  "Template": {
    "Type": "name_prefix_match"
  },
  "Service": {
    "Service": "\${name.full}",
    "Failover": {
      "NearestN": 3,
      "Datacenters": ["testc", "testa"]
    }
  }
}
EOF
}

function consul_join {
    $kubectl config use-context kind-testa
    consul=`$kubectl get pods -n consul-system | grep consul-server | grep Running`;
    while [ -z "$consul" ]; do
      echo "Waiting for testa consul pod";
      consul=`$kubectl get pods -n consul-system | grep consul-server | grep Running`;
      sleep 5;
    done
    $kubectl config use-context kind-testc
    consul=`$kubectl get pods -n consul-system | grep consul-server | grep Running`;
    while [ -z "$consul" ]; do
      echo "Waiting for testc consul pod";
      consul=`$kubectl get pods -n consul-system | grep consul-server | grep Running`;
      sleep 5;
    done
    # TODO: Again, if consul crashes, will it remember this join config and automatically
    # rejoin, or we have to monitor and rejoin ourselves ?
    $kubectl exec -it testc-consul-server-0 -n consul-system -- consul join -wan $testa_ip
}

function create_agent {
    name=$1
    agent=$2
    username=$3
    etchost_ip=$4
    etchost_name=$5
    services=$6

    docker run -d -it \
        -e NXT_GW_1_IP=$testa_ip -e NXT_GW_1_NAME=gateway.testa.nextensio.net \
        -e NXT_GW_2_IP=$testc_ip -e NXT_GW_2_NAME=gateway.testc.nextensio.net \
        -e NXT_GW_3_IP=$etchost_ip -e NXT_GW_3_NAME=$etchost_name \
        -e NXT_USERNAME=$username -e NXT_PWD=LetMeIn123 \
        -e NXT_AGENT=$agent -e NXT_CONTROLLER=$ctrl_ip:8080 \
        -e NXT_AGENT_NAME=$name -e NXT_SERVICES=$services \
        --network kind --name $name registry.gitlab.com/nextensio/agent/agent-deploy:latest
}

function create_all {
    # delete existing clusters
    kind delete cluster --name testa
    kind delete cluster --name testc
    kind delete cluster --name controller

    # Create a root CA
    openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -subj '/O=Nextensio Gateway/CN=gateway.*.nextensio.net' \
        -keyout $tmpdir/rootca.key -out $tmpdir/rootca.crt
    create_controller
    # Find controller ip address
    ctrl_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' controller-control-plane`
    bootstrap_controller $ctrl_ip

    create_cluster testa
    create_cluster testc
    # Find out ip addresses of testa cluster and testc cluster
    testa_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' testa-control-plane`
    testc_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' testc-control-plane`

    # Create dns entries inside kubernetes (coredns) for the gateway hostnames
    tmpf=$tmpdir/coredns.yaml
    cp coredns.yaml $tmpf
    sed -i "s/REPLACE_NODE1_IP/$testa_ip/g" $tmpf
    sed -i "s/REPLACE_NODE1_NAME/gateway.testa.nextensio.net/g" $tmpf
    sed -i "s/REPLACE_NODE2_IP/$testc_ip/g" $tmpf
    sed -i "s/REPLACE_NODE2_NAME/gateway.testc.nextensio.net/g" $tmpf

    # Configure the basic infrastructure elements in the cluster - like the loadbalancer,
    # coredns for DNS entries and the cluster manager itself
    bootstrap_cluster testa $testa_ip $ctrl_ip
    bootstrap_cluster testc $testc_ip $ctrl_ip

    # Finally, join the consuls in both clusters after ensuring their pods are Running
    consul_join

    # Configure consul in one cluster to query the remote cluster if local service lookup fails
    # Not sure if this needs to be done on both DCs, doing it anyways
    consul_query_config testa
    consul_query_config testc

    $kubectl config use-context kind-controller
    ctrlpod=`$kubectl get pods -n default | grep nextensio-controller | grep Running`;
    while [ -z "$ctrlpod" ]; do
      ctrlpod=`$kubectl get pods -n default | grep nextensio-controller | grep Running`;
      echo "Waiting for controller pod to be Running";
      sleep 5;
    done
    # configure the controller with some default customer/tenant information
    echo "Configuring the controller, may take a few seconds"
    ./ctrl.py $ctrl_ip $tmpdir
    echo "Controller config done, going to create agents and connectors"

    docker kill nxt-agent1; docker rm nxt-agent1
    docker kill nxt-agent2; docker rm nxt-agent2
    docker kill nxt-default; docker rm nxt-default
    docker kill nxt-kismis-ONE; docker rm nxt-kismis-ONE
    docker kill nxt-kismis-TWO; docker rm nxt-kismis-TWO
    docker container prune -f
    create_agent nxt-agent1 true test1@nextensio.net
    create_agent nxt-agent2 true test2@nextensio.net
    create_agent nxt-default false default@nextensio.net 127.0.0.1 foobar.com default-internet
    create_agent nxt-kismis-ONE false v1.kismis@nextensio.net 127.0.0.1 kismis.org v1-kismis-org
    create_agent nxt-kismis-TWO false v2.kismis@nextensio.net 127.0.0.1 kismis.org v2-kismis-org
    agent1_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' nxt-agent1`
    agent2_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' nxt-agent2`
}

function save_env {
    echo "###########################################################################"
    echo "######You can access controller UI at http://$ctrl_ip:3000/  ############"
    echo "##You can set a broswer proxy to $agent1_ip:8081 to send traffic via nextensio##"
    echo "##OR You can set a broswer proxy to $agent2_ip:8081 to send traffic via nextensio##"
    echo "##All the above information is saved in $tmpdir/environment for future reference##"
    
    envf=$tmpdir/environment
    echo "testa_ip=$testa_ip" > $envf
    echo "testc_ip=$testc_ip" >> $envf
    echo "ctrl_ip=$ctrl_ip" >> $envf
    echo "agent1_ip=$agent1_ip" >> $envf
    echo "agent2_ip=$agent2_ip" >> $envf
}

function main {
    image=$1
    if [ "$image" != "local" ];
    then
        download_images
    fi
    rm -rf $tmpdir/ 
    mkdir $tmpdir
    # Download kubectl
    curl -fsL https://storage.googleapis.com/kubernetes-release/release/v1.18.5/bin/linux/amd64/kubectl -o $tmpdir/kubectl
    chmod +x $tmpdir/kubectl
    # Download istioctl
    curl -fsL https://github.com/istio/istio/releases/download/1.6.4/istioctl-1.6.4-linux-amd64.tar.gz -o $tmpdir/istioctl.tgz
    tar -xvzf $tmpdir/istioctl.tgz -C $tmpdir/
    chmod +x $tmpdir/istioctl
    rm $tmpdir/istioctl.tgz
    curl -fsL https://get.helm.sh/helm-v3.4.0-linux-amd64.tar.gz -o $tmpdir/helm.tgz
    tar -zxvf $tmpdir/helm.tgz -C $tmpdir/
    chmod +x $tmpdir/linux-amd64/helm
    rm $tmpdir/helm.tgz
    # Create everything!
    create_all
    # Display and save environment information
    save_env
}

function usage {
    echo "create.sh usage : this will print this usage message"
    echo "create.sh : this will download images from gitlab and create the entire topology"
    echo "create.sh local-image : this will expect all images to be in local docker and create the entire topology"
    echo "create.sh reset-agent : this will restart the agent docker"
    echo "create.sh reset-conn : this will restart the connector(s) docker(s)"
}

options=$1
case "$options" in
*usage)
    usage
    ;;
*local-image)
    main local
    ;;
*reset-agent)
    source $tmpdir/environment
    docker kill nxt-agent1; docker rm nxt-agent1
    docker kill nxt-agent2; docker rm nxt-agent2
    docker container prune -f
    create_agent nxt-agent1 true test1@nextensio.net
    create_agent nxt-agent2 true test2@nextensio.net
    agent1_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' nxt-agent1`
    agent2_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' nxt-agent2`
    echo "##You can set a broswer proxy to $agent1_ip:8081 to send traffic via nextensio##"
    echo "##OR You can set a broswer proxy to $agent2_ip:8081 to send traffic via nextensio##"
    ;;
*reset-conn)
    source $tmpdir/environment
    docker kill nxt-default; docker rm nxt-default
    docker kill nxt-kismis-ONE; docker rm nxt-kismis-ONE
    docker kill nxt-kismis-TWO; docker rm nxt-kismis-TWO
    docker container prune -f
    create_agent nxt-default false default@nextensio.net 127.0.0.1 foobar.com default-internet
    create_agent nxt-kismis-ONE false v1.kismis@nextensio.net 127.0.0.1 kismis.org v1-kismis-org
    create_agent nxt-kismis-TWO false v2.kismis@nextensio.net 127.0.0.1 kismis.org v2-kismis-org
    ;;
*) 
    main remote
    ;;
esac

