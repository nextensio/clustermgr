# clustermgr

Manage/Configure nextensio cluster

## mel

mel is the manager of the cluster and responsible for configuring kubernetes 
in response to a new tenant or a new agent getting added/deleted etc.. The 
yaml files for mel in directory yaml/ here should be kept in some location
on the deployment server and it should be pointed to using the environment
variable YAML_DIR in file <HOME>/nextensio/cluster/environment

## test

The test directory contains utilities to create a nextension cluster on our
laptop which for all practical purposes is exactly the same as a cluster in
cloud. This local cluster can be used for development/testing/automation etc.
