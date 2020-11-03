#!/bin/bash

# This script is bundled into the mongodb image we use in the controller cluster
# Its just kept here for saving it in case the docker image gets blown off

function mongo_join {
mongo --eval "mongodb = ['mongodb-0-service.default.svc.cluster.local:$MONGODB_0_SERVICE_SERVICE_PORT', 'mongodb-1-service.default.svc.cluster.local:$MONGODB_1_SERVICE_SERVICE_PORT', 'mongodb-2-service.default.svc.cluster.local:$MONGODB_2_SERVICE_SERVICE_PORT']" --shell << EOL
cfg = {
        _id: "rs0",
        members:
            [
                {_id : 0, host : mongodb[0], priority : 1},
                {_id : 1, host : mongodb[1], priority : 0.9},
                {_id : 2, host : mongodb[2], priority : 0.5}
            ]
        }
rs.initiate(cfg)
EOL
}

mkdir /data/db/rs0-0

export POD_IP_ADDRESS=$(ifconfig | grep -E -o "inet\s\w{1,3}\.\w{1,3}\.\w{1,3}\.\w{1,3}.*broadcast" | grep -E -o "\w{1,3}\.\w{1,3}\.\w{1,3}\.\w{1,3}.*netmask" | grep -E -o "\w{1,3}\.\w{1,3}\.\w{1,3}\.\w{1,3}")
mongod --replSet rs0 --port 27017 --bind_ip localhost,$POD_IP_ADDRESS --dbpath /data/db/rs0-0 --oplogSize 128 --fork --logpath /var/log/mongodb/mongod.log

sleep 5
mongo_join 
while [[ $(mongo --quiet --eval "rs.conf()._id") != rs0 ]]
do
    echo "Retry mongo join" >> mongo.log
    sleep 5
    mongo_join
done
echo "Mongo joined succesfully" >> mongo.log

tail -f /dev/null

