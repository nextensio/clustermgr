package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const MinionImage = "minion:latest"

var dropDBScript = `var dbs = db.getMongo().getDBNames()
for(var i in dbs){
    db = db.getMongo().getDB( dbs[i] );
    db.dropDatabase();
}
`

func dropDB() {
	err := ioutil.WriteFile("/tmp/drop.js", []byte(dropDBScript), 0644)
	if err != nil {
		panic(err)
	}
	cmd := exec.Command("mongo", "/tmp/drop.js")
	cmd.Run()
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
func DBDelClusterConfig(tenant string) error {
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

func DBAddOneClusterBundle(tenant string, data *ClusterBundle) error {
	splits := strings.Split(data.Uid, ":")
	version := 1
	_, bundle := DBFindClusterBundle(tenant, splits[1])
	if bundle != nil {
		version = bundle.Version + 1
	}
	// The upsert option asks the DB to add if one is not found
	upsert := true
	after := options.After
	opt := options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
		Upsert:         &upsert,
	}
	result := bundleCltn.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": data.Uid},
		bson.D{
			{"$set", bson.M{"tenant": tenant, "version": version, "pod": data.Pod,
				"connectid": data.Connectid, "services": data.Services, "cpodrepl": data.CpodRepl}},
		},
		&opt,
	)
	if result.Err() != nil {
		return result.Err()
	}

	fmt.Println("Added connector")
	return nil
}

func DBDelOneClusterBundle(tenant string, bid string) error {
	id := tenant + ":" + bid
	_, err := bundleCltn.DeleteOne(
		context.TODO(),
		bson.M{"_id": id},
	)

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
		Image:    MinionImage,
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

func connMatch(sumConns, conns []ConnectorSummary) bool {
	for _, s := range sumConns {
		found := false
		for _, g := range conns {
			if s.Image == g.Image && s.Connectid == g.Connectid && s.CpodRepl == g.CpodRepl {
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

	if dbSum.Image != MinionImage || memSum.Image != MinionImage {
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
		t.Log("Connector content mismatch", connectors, dbSum.Connectors, memSum.Connectors)
		return false
	}

	return true
}

func connectId(tenant string, bid string) string {
	cid := strings.ReplaceAll(tenant+"-"+bid, "@", "-")
	cid = strings.ReplaceAll(cid, ".", "-")
	return cid
}

func CreateBundle(tenant string, bid string, cpodrepl int) ClusterBundle {
	uid := tenant + ":" + bid
	cid := connectId(tenant, bid)
	return ClusterBundle{
		Uid:       uid,
		Tenant:    tenant,
		Pod:       cid,
		Connectid: cid,
		Services:  []string{"google.com", "yahoo.com"},
		CpodRepl:  cpodrepl,
		Version:   0,
	}
}

func bundleYamlsMatch(t *testing.T, step string, tenant string, bundle string, match int) bool {
	matches, _ := filepath.Glob("/tmp/" + tenant + "/*" + bundle + "*")
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

func bundleYamlsRemoved(tenant string, bundle string) bool {
	matches, _ := filepath.Glob("/tmp/" + tenant + "/*" + bundle + "*")
	return len(matches) == 0
}

func tenantYamlsRemoved(tenant string) bool {
	matches, _ := filepath.Glob("/tmp/" + tenant + "/*")
	return len(matches) == 0
}

func cleanupFiles() {
	cmd := exec.Command("bash", "-c", "rm /tmp/*.yaml")
	cmd.Run()
	cmd = exec.Command("bash", "-c", "rm /tmp/nextensio/*.yaml")
	cmd.Run()
	cmd = exec.Command("bash", "-c", "rm /tmp/mel.*")
	cmd.Run()
}

// Basic test:
// 1. Add a tenant with 1 apod replica, test increase and decrease replicas
// 2. Add one bundle with 1 cpod replica, test increase and decrease replicas
// 3. Add one more bundle
// 4. Delete first bundle
// 5. Delete second bundle
// 6. Delete tenant
// TODO Liyakath - the sleeps here probably can be shortened a lot
// with the mongo changeset notifications because they will be much
// faster I think
func TestBasic(t *testing.T) {
	dropDB()
	// Remove files left over from previous iteration if any
	cleanupFiles()
	go melMain()
	// Let mel connect to DB and stuff, give it couple of seconds
	time.Sleep(2 * time.Second)
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
	if !tenantYamlsMatch(t, "apod1_1", "nextensio", 5) {
		t.Error()
		return
	}
	if !tenantSummaryMatch(t, "nextensio", 1, 1, nil) {
		t.Error()
		return
	}
	// Step 2 increase apod sets and replicas
	addTenant("nextensio", 2, 2)
	time.Sleep(2 * time.Second)
	if !tenantYamlsMatch(t, "apod2_2", "nextensio", 14) {
		t.Error()
		return
	}
	// Go back to Step 1
	addTenant("nextensio", 1, 1)
	time.Sleep(2 * time.Second)
	if !tenantYamlsMatch(t, "apod1_1", "nextensio", 5) {
		t.Error()
		return
	}

	// Step1: Add one bundle
	conn1 := CreateBundle("nextensio", "foobar@nextensio.com", 1)
	DBAddOneClusterBundle("nextensio", &conn1)
	time.Sleep(5 * time.Second)
	if !bundleYamlsMatch(t, "foobar1", "nextensio", "foobar", 9) {
		t.Error()
		return
	}
	cidFoobar := connectId("nextensio", "foobar@nextensio.com")
	csum := []ConnectorSummary{
		{Image: MinionImage, CpodRepl: 1, Connectid: cidFoobar},
	}
	if !tenantSummaryMatch(t, "nextensio", 1, 1, csum) {
		t.Error()
		return
	}
	// Step2: Increase Cpod replicas
	conn1 = CreateBundle("nextensio", "foobar@nextensio.com", 2)
	DBAddOneClusterBundle("nextensio", &conn1)
	time.Sleep(5 * time.Second)
	if !bundleYamlsMatch(t, "foobar2", "nextensio", "foobar", 11) {
		t.Error()
		return
	}
	csum = []ConnectorSummary{
		{Image: MinionImage, CpodRepl: 2, Connectid: cidFoobar},
	}
	if !tenantSummaryMatch(t, "nextensio", 1, 1, csum) {
		t.Error()
		return
	}
	// Go back to Step 1
	conn1 = CreateBundle("nextensio", "foobar@nextensio.com", 1)
	DBAddOneClusterBundle("nextensio", &conn1)
	time.Sleep(5 * time.Second)
	if !bundleYamlsMatch(t, "foobar1", "nextensio", "foobar", 9) {
		t.Error()
		return
	}
	cidFoobar = connectId("nextensio", "foobar@nextensio.com")
	csum = []ConnectorSummary{
		{Image: MinionImage, CpodRepl: 1, Connectid: cidFoobar},
	}
	if !tenantSummaryMatch(t, "nextensio", 1, 1, csum) {
		t.Error()
		return
	}

	// Add a second bundle
	conn2 := CreateBundle("nextensio", "kismis@nextensio.com", 2)
	DBAddOneClusterBundle("nextensio", &conn2)
	time.Sleep(5 * time.Second)
	if !bundleYamlsMatch(t, "kismis1", "nextensio", "kismis", 11) {
		t.Error()
		return
	}
	cidKismis := connectId("nextensio", "kismis@nextensio.com")
	csum = []ConnectorSummary{
		{Image: MinionImage, CpodRepl: 1, Connectid: cidFoobar},
		{Image: MinionImage, CpodRepl: 2, Connectid: cidKismis},
	}
	if !tenantSummaryMatch(t, "nextensio", 1, 1, csum) {
		t.Error()
		return
	}

	// Delete foobar bundle
	DBDelOneClusterBundle("nextensio", "foobar@nextensio.com")
	time.Sleep(2 * time.Second)
	if !bundleYamlsRemoved("nextensio", "foobar") {
		t.Error()
		return
	}

	// Delete kismis bundle
	DBDelOneClusterBundle("nextensio", "kismis@nextensio.com")
	time.Sleep(2 * time.Second)
	if !bundleYamlsRemoved("nextensio", "kismis") {
		t.Error()
		return
	}

	// Delete the tenant
	DBDelClusterConfig("nextensio")
	time.Sleep(10 * time.Second)
	if !tenantYamlsRemoved("nextensio") {
		t.Error()
		return
	}

	cleanupFiles()
}
