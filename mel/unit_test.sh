#!/usr/bin/env bash

export MY_POD_CLUSTER=gatewaytesta
export MY_YAML=../files/yaml/
export CONSUL_WAN_IP=1.1.1.1
export CONSUL_STORAGE_CLASS="default"
export TEST_ENVIRONMENT=true

# You can change the mongo URI to your own replicaset configuration or a cloud hosted mongo etc..
# If you use the mongo replicas in the kind-controller setup lets say at IP 172.18.0.2 as hard
# coded below, you will also have to add the below lines to your /etc/hosts
# 172.18.0.2 mongodb-0-service.default.svc.cluster.local
# 172.18.0.2 mongodb-1-service.default.svc.cluster.local
# 172.18.0.2 mongodb-2-service.default.svc.cluster.local
export MY_MONGO_URI=mongodb://172.18.0.2:27017,172.18.0.2:27018,172.18.0.2:27019/
export MY_JAEGER_COLLECTOR=none

sed -i -e "s|REPLACE_MONGO_URI|$MY_MONGO_URI|g" ./test/yamls/nextensio/apod1_1/deploy-nextensio-apod1.yaml
sed -i -e "s|REPLACE_MONGO_URI|$MY_MONGO_URI|g" ./test/yamls/nextensio/foobar2/deploy-nextensio-foobar-nextensio-com.yaml
sed -i -e "s|REPLACE_MONGO_URI|$MY_MONGO_URI|g" ./test/yamls/nextensio/foobar1/deploy-nextensio-foobar-nextensio-com.yaml
sed -i -e "s|REPLACE_MONGO_URI|$MY_MONGO_URI|g" ./test/yamls/nextensio/apod2_2/deploy-nextensio-apod1.yaml
sed -i -e "s|REPLACE_MONGO_URI|$MY_MONGO_URI|g" ./test/yamls/nextensio/apod2_2/deploy-nextensio-apod2.yaml
sed -i -e "s|REPLACE_MONGO_URI|$MY_MONGO_URI|g" ./test/yamls/nextensio/kismis1/deploy-nextensio-kismis-nextensio-com.yaml

# We cant let go test run all of the tests in paralell because
# all of them use the same "mel". So run them serially here
go test -run TestBasicWithNoErrors
go test -run TestBasicWithKubeErrors
go test -run TestBasicWithMongoErrors

git checkout -- ./test/yamls/nextensio/apod1_1/deploy-nextensio-apod1.yaml
git checkout -- ./test/yamls/nextensio/foobar2/deploy-nextensio-foobar-nextensio-com.yaml
git checkout -- ./test/yamls/nextensio/foobar1/deploy-nextensio-foobar-nextensio-com.yaml
git checkout -- ./test/yamls/nextensio/apod2_2/deploy-nextensio-apod1.yaml
git checkout -- ./test/yamls/nextensio/apod2_2/deploy-nextensio-apod2.yaml
git checkout -- ./test/yamls/nextensio/kismis1/deploy-nextensio-kismis-nextensio-com.yaml
