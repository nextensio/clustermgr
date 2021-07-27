package main

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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

const chunkSize = 64000

func fileDiff(file1, file2 string) bool {
	f1, err := os.Open(file1)
	if err != nil {
		log.Fatal(err)
	}
	defer f1.Close()

	f2, err := os.Open(file2)
	if err != nil {
		log.Fatal(err)
	}
	defer f2.Close()

	for {
		b1 := make([]byte, chunkSize)
		_, err1 := f1.Read(b1)

		b2 := make([]byte, chunkSize)
		_, err2 := f2.Read(b2)

		if err1 != nil || err2 != nil {
			if err1 == io.EOF && err2 == io.EOF {
				return true
			} else if err1 == io.EOF || err2 == io.EOF {
				return false
			} else {
				log.Fatal(err1, err2)
			}
		}

		if !bytes.Equal(b1, b2) {
			return false
		}
	}
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

// This API will add a new doc or update one for pods allocated to a tenant
// within a specific cluster
func DBAddClusterConfig(data *ClusterConfig) error {
	version := 1
	Cluster := data.Cluster
	err, clc := DBFindClusterConfig(data.Tenant)
	if err != nil {
		return err
	}
	if clc != nil {
		// If ClusterConfig exists, use following fields
		version = clc.Version + 1
	}

	// The upsert option asks the DB to add if one is not found
	upsert := true
	after := options.After
	opt := options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
		Upsert:         &upsert,
	}
	result := clusterCfgCltn.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": data.Tenant},
		bson.D{
			{"$set", bson.M{"_id": data.Tenant,
				"apodsets": data.ApodSets, "apodrepl": data.ApodRepl,
				"image":   data.Image,
				"cluster": Cluster, "tenant": data.Tenant, "version": version}},
		},
		&opt,
	)
	if result.Err() != nil {
		return result.Err()
	}

	return nil
}

// Find the ClusterConfig doc for a tenant within a cluster
func DBFindClusterConfig(tenant string) (error, *ClusterConfig) {
	var clcfg ClusterConfig
	err := clusterCfgCltn.FindOne(context.TODO(), bson.M{"_id": tenant}).Decode(&clcfg)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	return nil, &clcfg
}

// Delete the ClusterConfig doc for a tenant within a cluster.
func DBDelClusterConfig(clid string, tenant string) error {
	err, clcfg := DBFindClusterConfig(tenant)
	if err != nil {
		return err
	}
	if clcfg == nil {
		return nil
	}

	_, err = clusterCfgCltn.DeleteOne(context.TODO(), bson.M{"_id": tenant})

	return err
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

func yamlsPresent() bool {
	consulPresent := false
	istioPresent := false
	igwPresent := false

	if _, err := os.Stat("/tmp/consul.yaml"); err == nil {
		consulPresent = true
	}

	if _, err := os.Stat("/tmp/istio_flow_control.yaml"); err == nil {
		istioPresent = true
	}

	if _, err := os.Stat("/tmp/igw.yaml"); err == nil {
		igwPresent = true
	}

	return consulPresent && istioPresent && igwPresent
}

func egwYamlsMatch(t *testing.T) bool {
	matches, _ := filepath.Glob("/tmp/*gatewaytesta*")
	if len(matches) != 0 {
		// We should not have ourselves as egress destination
		return false
	}
	count := 0
	matches, _ = filepath.Glob("/tmp/*gatewaytestc*")
	for _, match := range matches {
		if !fileDiff(match, "./test/yamls/"+filepath.Base(match)) {
			t.Logf("File mismatch %s", match)
			t.Error()
			return false
		}
		count++
	}
	return count == 3
}

func addTenant(name string, apodrepl int, apodsets int) {
	cfg := ClusterConfig{
		Id:       name,
		Cluster:  MyCluster,
		Tenant:   name,
		Image:    "foobar",
		ApodRepl: apodrepl,
		ApodSets: apodsets,
		Version:  0,
	}
	DBAddClusterConfig(&cfg)
}

func tenantYamlsPresent(tenant string) bool {
	flowControlPresent := false
	if _, err := os.Stat("/tmp/" + tenant + "/flow_control.yaml"); err == nil {
		flowControlPresent = true
	}
	return flowControlPresent
}

func tenantYamlsMatch(t *testing.T, step string, tenant string, match int) bool {
	matches, _ := filepath.Glob("/tmp/" + tenant + "/*apod*")
	count := 0
	for _, match := range matches {
		if !fileDiff(match, "./test/yamls/"+tenant+"/"+step+"/"+filepath.Base(match)) {
			t.Logf("File mismatch %s", match)
			t.Error()
			return false
		}
		count++
	}
	return count == match
}

func remoteGwMatch(sumGws, gws []string) bool {
	for _, s := range sumGws {
		found := false
		for _, g := range gws {
			if s == g {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func connMatch(sumConns, conns []ConnectorSummary) bool {
	for _, s := range sumConns {
		found := false
		for _, g := range conns {
			if s == g {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Verify the "in memory" tenant summary (white box testing) AND also verify
// the "in databse" tenant summary (black box testing)
func tenantSummaryMatch(t *testing.T, tenant string, apodrepl int, apodsets int, connectors []ConnectorSummary) bool {
	_, dbSum := DBFindTenantSummary(tenant)
	memSum := tenants[tenant].tenantSummary

	if dbSum.Image != "foobar" || memSum.Image != "foobar" {
		t.Log("Image mismatch", dbSum.Image, memSum.Image)
		return false
	}
	if dbSum.ApodRepl != apodrepl || memSum.ApodRepl != apodrepl {
		t.Log("Apod repl mismatch", dbSum.ApodRepl, memSum.ApodRepl)
		return false
	}
	if dbSum.ApodSets != apodsets || memSum.ApodSets != apodsets {
		t.Log("Apod set mismatch", dbSum.ApodSets, memSum.ApodSets)
		return false
	}
	if dbSum.Tenant != tenant || memSum.Tenant != tenant {
		t.Log("tenant mismatch", dbSum.Tenant, memSum.Tenant)
		return false
	}
	if len(dbSum.Connectors) != len(connectors) || len(memSum.Connectors) != len(connectors) {
		t.Log("Connector mismatch", len(dbSum.Connectors), len(memSum.Connectors))
		return false
	}
	if !connMatch(dbSum.Connectors, connectors) || !connMatch(memSum.Connectors, connectors) {
		t.Log("Connector content mismatch")
		return false
	}

	return true
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
	time.Sleep(2 * time.Second)
	if !yamlsPresent() {
		t.Error()
		return
	}
	if !egwYamlsMatch(t) {
		t.Error()
		return
	}
	// Step 1
	addTenant("nextensio", 1, 1)
	time.Sleep(2 * time.Second)
	if !tenantYamlsPresent("nextensio") {
		t.Error()
		return
	}
	if !tenantYamlsMatch(t, "step1", "nextensio", 5) {
		t.Error()
		return
	}
	if !tenantSummaryMatch(t, "nextensio", 1, 1, nil) {
		t.Error()
		return
	}
	// Step 2
	addTenant("nextensio", 2, 2)
	time.Sleep(2 * time.Second)
	if !tenantYamlsMatch(t, "step2", "nextensio", 14) {
		t.Error()
		return
	}
	//time.Sleep(100 * time.Second)
}
