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

# Create a controller
function create_controller {
    kind create cluster --config ./kind-config.yaml --name controller

    # Get the controller and UI/UX images
    docker pull registry.gitlab.com/nextensio/ux/ux-deploy:latest
    docker pull registry.gitlab.com/nextensio/controller/controller-test:latest

    kind load docker-image registry.gitlab.com/nextensio/ux/ux-deploy:latest --name controller
    kind load docker-image registry.gitlab.com/nextensio/controller/controller-test:latest --name controller

    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/namespace.yaml
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/metallb.yaml
    $kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"
    $kubectl apply -f https://raw.githubusercontent.com/haproxytech/kubernetes-ingress/master/deploy/haproxy-ingress.yaml
}

function bootstrap_controller {
    my_ip=$1

    $kubectl config use-context kind-controller

    tmpf=$tmpdir/controller.yaml
    cp controller.yaml $tmpf
    sed -i "s/REPLACE_SELF_NODE_IP/$my_ip/g" $tmpf
    $kubectl apply -f $tmpf
}

# Create kind clusters for testa and testc
function create_cluster {
    cluster=$1

    # Create a docker-in-docker kubernetes cluster with one master and one worker
    kind create cluster --config ./kind-config.yaml --name $cluster

    # This is NOT the right thing to do in real deployment, either we should limit the 
    # roles (RBAC) of the clustermgr or even better make clustermgr use kube APIs instead
    # of kubectl
    $kubectl create clusterrolebinding permissive-binding \
        --clusterrole=cluster-admin \
        --user=admin \
        --user=kubelet \
        --group=system:serviceaccounts

    # Install istio and metallb
    $istioctl manifest apply --set profile=demo --set values.global.proxy.accessLogFile="/dev/stdout"
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/namespace.yaml
    $kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/metallb.yaml
    $kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"

    # get docker images
    docker pull registry.gitlab.com/nextensio/cluster/minion:latest
    docker pull registry.gitlab.com/nextensio/clustermgr/mel-deploy:latest

    # kind needs all images locally present, it wont download from any registry
    kind load docker-image registry.gitlab.com/nextensio/cluster/minion:latest --name $cluster
    kind load docker-image registry.gitlab.com/nextensio/clustermgr/mel-deploy:latest --name $cluster
}

function bootstrap_cluster {
    cluster=$1
    my_ip=$2
    ctrl_ip=$3

    $kubectl config use-context kind-$cluster

    # Deploy the clustr manager "mel"
    tmpf=$tmpdir/$cluster-mel.yaml
    cp mel.yaml $tmpf
    sed -i "s/REPLACE_CLUSTER/$cluster/g" $tmpf
    sed -i "s/REPLACE_SELF_NODE_IP/$my_ip/g" $tmpf
    sed -i "s/REPLACE_CONTROLLER_IP/$ctrl_ip/g" $tmpf
    $kubectl apply -f $tmpf

    # Install loadbalancer to direct traffic to istio ingress gateway
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
    $kubectl replace -n kube-system -f $tmpf
}

# Setup prepared query so that consul forwards the dns lookup to multiple DCs
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
    $kubectl exec -it testc-consul-server-0 -n consul-system -- consul join -wan $testa_ip
}

function main {
    create_controller
    create_cluster testa
    create_cluster testc

    # Find out ip addresses of controller, testa cluster and testc cluster
    testa_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' testa-worker`
    testc_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' testc-worker`
    ctrl_ip=`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' controller-control-plane`

    bootstrap_controller $ctrl_ip

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
      echo "Waiting for controller pod to be Running";
      ctrlpod=`$kubectl get pods -n default | grep nextensio-controller | grep Running`;
      sleep 5;
    done
    # configure the controller with some default customer/tenant information
    ./ctrl.py $ctrl_ip
}

rm -rf $tmpdir/ 
mkdir $tmpdir
# Download kubectl
curl -fsL https://storage.googleapis.com/kubernetes-release/release/v1.18.5/bin/linux/amd64/kubectl -o $tmpdir/kubectl
chmod +x $tmpdir/kubectl
# Download istioctl
curl -fsL https://github.com/istio/istio/releases/download/1.6.4/istioctl-1.6.4-linux-amd64.tar.gz -o $tmpdir/istioctl.tgz
tar -xvzf $tmpdir/istioctl.tgz -C $tmpdir/
chmod +x $tmpdir/istioctl
# Create everything!
main

echo "###########################################################################"
echo "########## ADD THE BELOW TWO LINES IN YOUR /etc/hosts FILE ################"
echo $testa_ip gateway.testa.nextensio.net
echo $testc_ip gateway.testc.nextensio.net
echo "###########################################################################"
echo "######You can access controller UI at http://$ctrl_ip:3000/  ############"

