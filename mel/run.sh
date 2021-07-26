#!/usr/bin/env bash

export MY_POD_CLUSTER=gatewaytesta
export MY_YAML=../files/yaml/
export CONSUL_WAN_IP=1.1.1.1
export CONSUL_STORAGE_CLASS="default"
export MY_MONGO_URI=mongodb://localhost:27017
export TEST_ENVIRONMENT=true
go test
