package main

import (
	"context"
	"io/ioutil"
	"os/exec"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var dropDBScript = `var dbs = db.getMongo().getDBNames()
for(var i in dbs){
    db = db.getMongo().getDB( dbs[i] );
    print( "dropping db " + db.getName() );
    db.dropDatabase();
}
`

func dropDB() {
	err := ioutil.WriteFile("/tmp/drop.js", []byte(dropDBScript), 0644)
	if err != nil {
		panic(err)
	}
	exec.Command("mongo", "/tmp/drop.js")
}

// Find gateway/cluster doc given the gateway name
func DBAddGatewayCluster(gw ClusterGateway) error {
	e, find := DBFindGatewayCluster(gw.Name)
	if e != nil {
		return e
	}
	version := 1
	if find != nil {
		version = find.Version + 1
	}
	// The upsert option asks the DB to add if one is not found
	upsert := true
	after := options.After
	opt := options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
		Upsert:         &upsert,
	}
	err := clusterGwCltn.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": gw.Name},
		bson.D{
			{"$set", bson.M{"cluster": gw.Cluster, "version": version, "remotes": gw.Remotes}},
		},
		&opt,
	)
	if err.Err() != nil {
		return err.Err()
	}

	return nil
}

func addGateways() {
	gw := ClusterGateway{
		Name:    "gatewaytesta.nextensio.net",
		Cluster: "gatewaytesta",
		Version: 1,
		Remotes: []string{"gatewaytestc"},
	}
	DBAddGatewayCluster(gw)

	gw = ClusterGateway{
		Name:    "gatewaytestc.nextensio.net",
		Cluster: "gatewaytestc",
		Version: 1,
		Remotes: []string{"gatewaytesta"},
	}
	DBAddGatewayCluster(gw)
}
func TestMelNoErrors(t *testing.T) {
	dropDB()
	go melMain()
	for {
		if !dbConnected {
			time.Sleep(time.Second)
		} else {
			break
		}
	}
	addGateways()
	for {
		time.Sleep(1000000 * time.Second)
	}
}
