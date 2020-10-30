
# Development and Testing philosophy

In a discussion with one of the creators of Kubernetes, he mentioned that one of the foremost 
design goals of Kubernetes was to "run anywhere, run the same" - ie the software should run on
a cheap raspberry pi or a high end AWS server, and it should run and behave exactly the same. 
And he mentioned that as being one of the reasons behind the success of Kubernetes!

We at Nextensio develope our products with the same philosophy and mindset - if someone says that
"oh I need this blah blah aws instance to have a production-like environment", then we have some
problem with our thinking and architecture. We use Kubernetes heavily, and that should enable us
to run anywhere. 

With that goal in mind, we provide a "Kubernetes In Docker" (kind) based environment where we
can run all the nextensio components on our laptop. And we provide mechanisms to create reasonably
comprehensive nextensio topologies in an automated way - its cheap to create and cheap to tear 
down, so do it as much as you like, as often as you like

Over time, we will enhance this mechanism to be used for automation testing etc.. - automation
testing is another philosophy to rant on. Automation IS a responsibility of the developer,
whoever adds a feature, needs to automate it themselves.

## What is kind

There is enough literature on internet. In summary, Kubernetes needs "nodes" to run "pods" (containers).
Think of a node as a physical server. Kind creates a docker container and uses that as a 'node'. 
This is possible because docker (or rather linux containers) allows containers inside containers.
So kind treats a top level  container as a Kubernetes node and runs Kubernetes pods inside that
container, pretty neat and useful !

The beauty of the kind based setup is that even if there is some screwup where the wrong kubernetes
rules are installed or some messup happens, we just need to delete the kind clusters by saying 
kind delete cluster --name testa; kind delete cluster --name testc; kind delete cluster --name controller
and we are back to normal - there is NOTHING changed on the host ubuntu, so there is no messup that 
cannot be recovered by deleting the kind clusters !

## Laptop horsepower requirements

It is worth a few extra 100$ investment in a powerful laptop. I have a 2.2GH 6 core Thinkpad extreme
with 32G ram and 1TB SSD. I am not saying that is required - even after creating the entire topology,
my cpu usage is barely 10% and I have more than 24G free memory. So you can run on smaller machines 
also. I run in an Ubuntu 18.04 in a windows10 hyper-V VM, it should work on any ubuntu - just dont 
create your VM with one vCPU, give it as many vCPUs as you have cores and give it like 75% RAM available
on your host.

## Installing the setup

### Pre Requisites 

* install docker - plenty of docs on internet to do that

* docker login registry.gitlab.com - the script uses images for each of the components - controller, UI/UX, cluster/minion 
and clustermgr/mel - all stored in gitlab. Today they are manually built and kept there, but its easy to automate
that with gitlab CI/CD. So you need to login to the image repo to be able to allow the scripts to download the 
images, this is a one time activity

* install kind (https://kind.sigs.k8s.io/docs/user/quick-start/) - the kind command needs to be in $PATH

Thats about it, you are ready to create the full nextensio setup!

### Creating the setup

Get the nextensio clustermgr repository, go to test/kind folder and run "./create.sh" - give it good 15 minutes, 
initially it will download all images from the registry, once its downloaded it will use from the local docker cache
in subsequent attempts.

The other thing that takes a lot of time is installing istio into the Kind cluster, I wish there was a kind 
cluster available with istio pre-installed, its a TODO to try and make one ourselves to save a lot of time
thats needed to create the setup.

Once the script is done, it will ask you to add the gateway domain names and IPs in /etc/hosts, it will also
tell you how to access the nextensio UI from your browser

The test/kind directory has some very basic kubernetes yamls which are required for basic connectivity etc.. -
like we need a loadbalancer for the cluster to work, so there is a metallb.yaml which configures the loadbalancer.
Other than that, all the nextensio specific yamls are generated and configured on the fly automatically by the
clustermgr. Note that the clustermgr has been given a clusterrolebinding of admin in the create.sh script so 
that custermgr can also run kubectl and modify its own cluster. Later we need to move away from kubectl and use
Kube APIs instead

### Deleting the setup

Very simple, type the below

kind delete cluster --name testa; kind delete cluster --name testc; kind delete cluster --name controller

### What actually gets created

The automated scripts do the following today.

* Creates one kind Kubernetes cluster to run the controller and UI. The name of that kind cluster is
'controller'. kind get clusters will show the name, a docker ps will also show the name. Inside this 
cluster, one pod will be running the controller code, one pod will be running the UI/UX code. The 
controller will come preconfigured with a user named 'agent-1' and a connector named 'default'. Anyone
is free to point their browsert to the IP of the controller docker instance at port 3000, access the UI
and make more config changes as required for their development/testing. The controller pod will also 
run a mongodb instance which is accesible via the docker instance IP and port 27017. Very soon we might
run mongodb as kubernetes pods in this cluster, with replication turned on so that we can test mongodb
changeset functionality which needs replication turned on

* Creates two kind kubernetes clusters each of which are basically the nextensio POPs. These clusters 
run two nextensio pods - one pod being the "clustermgr" (called "mel") which manages creation of kubernetes 
rules and services in response to what is configured on the controller. And another pod being the "minion"
which does the actual packet forwarding magic sauce of nextensio.

* As mentioned earlier the script also configures the cluster via python APIs (ctrl.py) with a basic set
of users and connectors and policies etc.. And the clustermgr automatically configures the cluster with
kubernetes policies corresponding to the users and connectors etc..

## What can be done today

Once the clusters are all created, at the end of the script run, it will clearly say a message asking us to
add the gateway domain names to our /etc/host file so we can use the domain names in our test cases.

The script will output the address of two agents at port 8081. You can point your browser to either of 
the agents and browse internet, that all goes via the local nextensio clusters ! And you can use the 
nextensio UI to add / modify the routing rules for the agents etc.. There is also a kismis.org internal
website hosted on two docker containers nxt-kismis-ONE and nxt-kismis-TWO, agent1 goes to kismis-ONE and
agent2 to kismis-TWO, both showing different data for the same website URL !

## FAQs

* How do I see details of each cluster ?

To see a cluster, first set the kubernetes context by saying 'kubectl config use-context kind-testa' or
'kubectl config use-context kind-testc' or 'kubectl config use-context kind-controller' - testa and testc
are the two Nextensio POPs and the kind-controller is the controller cluster

After you do the use-context command, then you can say 'kubect get pods --all-namespaces' or run any
other standard kubectl commands to see details of the cluster - or you can run 'kubectl exec -it ...' 
to login into a pod etc.. - of course this assumes you have the small kubetl utility installed

* How do a restart a pod

login into the pod (kubectl exec -it ... ) and 'kill -TERM 1'

* I have to test an image, how do I get a pod to run my image ?

First of all, go to whichever repository you are working on, look for a Makefile and there will be a 
'make <something>' to create a docker image out of that repository. So make your changes and make a 
docker image. Then tag that docker image with the label 'latest' - for example the minion is as below,
'docker tag 24ef21e0a80c registry.gitlab.com/nextensio/cluster/minion:latest'

Once its tagged on your host, you need to upload that image to the kind cluster - the kind cluster 
maintains only a local repository, it will not be able to access the host docker repository, so say
'kind load docker-image registry.gitlab.com/nextensio/cluster/minion:latest --name <cluster-name>'

Now if its a clustermgr or a controller or a UI/UX stuff you are debugging, those pods are created 
using yamls which the creation script has stored in /tmp/nextensio-kind .. Find the "Deployment" 
yamls in that and just say 'kubect delete -f <just the Deployment yaml>' and 
'kubectl apply -f <just the Deployment yaml>' - that will delete and create the pod and when its created,
it will use your new image.

Now if its a minion pod you want to debug, minions are launched by the clustermgr - because the controller
is what decides how many pods each tenant needs etc.., so clustermgr reads that config and launches as many
pods as required. So those yamls are not in /tmp/nextensio-kind. As before, the first step is to create
your docker image, tag it as latest and kind load docker-image to both testa and testc clusters. Then lets 
say you are going to upgrade testa first, so you say 'kubectl config use-context kind-testa' and then find 
the pod names 'clustermgr' and get a shell to that pod (kubectl exec -it ... -- /bin/sh) and then check for 
yamls in /tmp/ in that pod. You will find the Deployment yamls and do the same delete and apply again.

NOTE1: Once you build your own image and tag it as latest, and next time you want that to be used when
creating a brand new setup, make sure to say 'create.sh local' - ie ensure that docker doesnt download
images from gitlab and just use the local images

NOTE2: I plead ignorance here, I dont know any other easy way to restart/upgrade a pod other than delete/apply,
so if anyone knows a better way please do update this section with the easier method

* I want to create a cluster with some image thats not the ones in gitlab labelled latest

Well, we will figure a way out going forward to specify images of your choice, today it just picks the 
image labelled :latest from gitlab, feel free to modify create.sh for the time being with the image of
your choice

* My minion pods are crashing 

The most usual culprit is mongodb access. Today the OPA code barfs if it cant connect to mongodb. So from
your host (outside docker), try 'curl http://<controller ip>:27017 - if you see an output that means the
connectivity is fine and its something else.

The other culprit is that the OPA code barfs if it doesnt find a user attribute or a bundle attribute, 
for example we configure a user and forgot to configure attributes, OPA will barf. Or we forgot to configure
AccessPolicy, OPA will barf. Although I think ashwin has fixed it recently.

* What do I do if a pod keeps crashing frequenty ?

Basically you need to create a docker image for that pod which does not start the application by default.
Instead it will just launch some dummy program that sleeps for ever (like a "tail -f /dev/null"). And then
you can shell into that pod (kubectl exec -it ...) and start your app manually and debug why its crashing
etc.. Follow the previous instructions on how to upload the docker image and restart pods etc.. And for tips 
on how to create such a docker image with a dummy start program, see the Dockerfile.debug example in the 
cluster repository, it starts a minion pod where you have to launch minion manually

* What do I do if I cant browse via my proxy ?

Well, if you read this, you are a nextensio engineer - you have to debug the problem and get to some understanding
of what failed so we can fix it. So the below steps are just pointers on the "places to look for" and a 
suggestion to restart the "places" if it has a problem, but do not restart until you have figured out what
the problem is - we can only get better by fixing one issue at a time, its fine if it takes long time 
initially to figure things out, it will get easier only if we go through that process

1. Lets start with agent first, assuming you have pointed your proxy to agent1, login to the agent docker
by saying 'docker exec -it nxt-agent2 /bin/bash' and do a 'tail -f /var/log/agent.log' and confirm that 
every 10 seconds you see the message 'websocket is still open'. Otherwise something happened to the web
socket or something happened to the onboarding etc.. - we have code in place to retry everyting in case
of errors, but its still getting hardened and better-ed as we speak. If the agent has an issue, then
do "create.sh reset-agent" and that will restart both the agents, try again now.

2. Do the same above for connector, if connector websocket has issues, "create.sh reset-conn" and try again

3. Sometimes the ubuntu proxy setting gets into a wierd state - so just disable ubuntu proxy and enable
it again and see if it works

4. If none of that is the case, you can try restarting the minion pods in either the agent or connector
clusters or both
