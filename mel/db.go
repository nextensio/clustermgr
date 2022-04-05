package main

import (
	"context"
	"errors"

	"github.com/golang/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var dbClient *mongo.Client

// Collections for global operational info - clusters/gateways and tenants
var clusterGwCltn *mongo.Collection
var clusterCfgCltn *mongo.Collection

// Collections specific to this cluster for tracking users and services
var clusterDB *mongo.Database
var bundleCltn *mongo.Collection
var summaryCltn *mongo.Collection
var errRecCltn *mongo.Collection

func ClusterGetDBName(cl string) string {
	return ("Cluster-" + cl + "-DB")
}

func DBConnect() bool {
	var err error
	dbClient, err = mongo.NewClient(options.Client().ApplyURI(MyMongo))
	if err != nil {
		glog.Error("Database client create failed")
		return false
	}

	err = dbClient.Connect(context.TODO())
	if err != nil {
		glog.Error("Database connect failed")
		return false
	}
	err = dbClient.Ping(context.TODO(), readpref.Primary())
	if err != nil {
		glog.Errorf("Database ping error - %s", err)
		return false
	}

	clusterDB = dbClient.Database(ClusterGetDBName(MyCluster))
	bundleCltn = clusterDB.Collection("NxtConnectors")
	summaryCltn = clusterDB.Collection("NxtTenantSummary")
	clusterCfgCltn = clusterDB.Collection("NxtTenants")
	clusterGwCltn = clusterDB.Collection("NxtGateways")
	errRecCltn = clusterDB.Collection("NxtErrRec")

	return true
}

type ConnectorSummary struct {
	Id        string `bson:"_id"`
	Image     string `bson:"image"`
	Connectid string `bson:"connectid"`
	CpodRepl  int    `bson:"cpodrepl"`
}

type TenantSummary struct {
	Tenant     string             `bson:"_id"`
	Image      string             `bson:"image"`
	ApodRepl   int                `bson:"apodrepl"`
	ApodSets   int                `bson:"apodsets"`
	Connectors []ConnectorSummary `bson:"connectors"`
}

func DBFindAllTenantSummary() (error, []TenantSummary) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	var summary []TenantSummary

	cursor, err := summaryCltn.Find(context.TODO(), bson.M{})
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	err = cursor.All(context.TODO(), &summary)
	if err != nil {
		return err, nil
	}

	return nil, summary
}

func DBFindTenantSummary(tenant string) (error, *TenantSummary) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	var summary TenantSummary

	err := summaryCltn.FindOne(
		context.TODO(),
		bson.M{"_id": tenant},
	).Decode(&summary)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	return nil, &summary
}

func DBUpdateTenantSummary(tenant string, summary *TenantSummary) error {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error")
		}
	}

	// The upsert option asks the DB to add if one is not found
	upsert := true
	after := options.After
	opt := options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
		Upsert:         &upsert,
	}
	err := summaryCltn.FindOneAndUpdate(
		context.TODO(),
		bson.M{"_id": tenant},
		bson.D{
			{"$set", summary},
		},
		&opt,
	)

	if err.Err() != nil {
		return err.Err()
	}

	return nil
}

func DBDeleteTenantSummary(tenant string) error {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error")
		}
	}

	_, err := summaryCltn.DeleteOne(
		context.TODO(),
		bson.M{"_id": tenant},
	)

	if err != nil {
		return err
	}
	return nil
}

// NOTE: The bson decoder will not work if the structure field names dont start with upper case
type ClusterGateway struct {
	Name    string   `json:"name" bson:"_id"`
	Cluster string   `json:"cluster" bson:"cluster"`
	Version int      `json:"version" bson:"version"`
	Remotes []string `json:"remotes" bson:"remotes"`
}

// Find gateway/cluster doc given the gateway name
func DBFindGatewayCluster(gwname string) (error, *ClusterGateway) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	var gateway ClusterGateway
	err := clusterGwCltn.FindOne(
		context.TODO(),
		bson.M{"_id": gwname},
	).Decode(&gateway)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	return nil, &gateway
}

type ClusterConfig struct {
	Id       string `json:"id" bson:"_id"` //TenantID
	Cluster  string `json:"cluster" bson:"cluster"`
	Tenant   string `json:"tenant" bson:"tenant"`
	Image    string `json:"image" bson:"image"`
	ApodRepl int    `json:"apodrepl" bson:"apodrepl"`
	ApodSets int    `json:"apodsets" bson:"apodsets"`
	Version  int    `json:"version" bson:"version"`
}

// Find a specific tenant  within a cluster
func DBFindTenantInCluster(tenant string) (error, *ClusterConfig) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	var clcfg ClusterConfig
	err := clusterCfgCltn.FindOne(
		context.TODO(),
		bson.M{"tenant": tenant},
	).Decode(&clcfg)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	return nil, &clcfg
}

// Find all tenants present in a cluster
func DBFindAllTenantsInCluster() (error, []ClusterConfig) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	var clcfg []ClusterConfig
	cursor, err := clusterCfgCltn.Find(context.TODO(), bson.M{})
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	err = cursor.All(context.TODO(), &clcfg)
	if err != nil {
		return err, nil
	}
	if len(clcfg) > 0 {
		return nil, clcfg
	}
	return nil, nil
}

// The Pod here indicates the "pod set" that this user should
// connect to, each pod set has its own number of replicas etc..
type ClusterBundle struct {
	Uid       string   `json:"uid" bson:"_id"`
	Tenant    string   `json:"tenant" bson:"tenant"`
	Pod       string   `json:"pod" bson:"pod"`
	Connectid string   `json:"connectid" bson:"connectid"`
	Services  []string `json:"services" bson:"services"`
	Version   int      `json:"version" bson:"version"`
	CpodRepl  int      `json:"cpodrepl" bson:"cpodrepl"`
}

// Find a specific tenant's connector within a cluster
func DBFindClusterBundle(tenant string, bundleid string) (error, *ClusterBundle) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	bid := tenant + ":" + bundleid
	var bundle ClusterBundle
	err := bundleCltn.FindOne(
		context.TODO(),
		bson.M{"_id": bid},
	).Decode(&bundle)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	return nil, &bundle
}

func DBFindAllClusterBundlesForTenant(tenant string) (error, []ClusterBundle) {
	if unitTesting {
		mongoErr := GetEnv("TEST_MONGO_ERR", "NOT_TEST")
		if mongoErr == "true" {
			glog.Error("Mongo UT error")
			return errors.New("Mongo unit test error"), nil
		}
	}

	var bundles []ClusterBundle

	cursor, err := bundleCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return err, nil
	}
	err = cursor.All(context.TODO(), &bundles)
	if err != nil {
		return err, nil
	}

	return nil, bundles
}

//---------------------------Tenant ErrRec Collection functions---------------------------

type ErrRec struct {
	Tenant     string
	Connectid  string
	Operation  string
	Collection string
	Error      string
	ChangeAt   string
}

// Today there is either errors per tenant or there is errors for gateways (applicable to all tenants)
// The fact that the error is NOT for a tenant is indicated by tenant "" and connectid "". Of course later
// if we have more kind of errors, this will need changing
func DBErrToKey(data *ErrRec) string {
	key := ""
	if data.Tenant == "" && data.Connectid == "" {
		key = "gateway-"
	} else if data.Tenant != "" {
		key = "tenant-" + data.Tenant
	}
	return key
}

func DBAddErrRec(data *ErrRec) error {
	// The upsert option asks the DB to add if one is not found
	upsert := true
	after := options.After
	opt := options.FindOneAndUpdateOptions{
		ReturnDocument: &after,
		Upsert:         &upsert,
	}
	err := errRecCltn.FindOneAndUpdate(
		context.TODO(),
		bson.M{"key": DBErrToKey(data)},
		bson.D{
			{"$set", bson.M{"key": DBErrToKey(data), "changeat": data.ChangeAt}},
		},
		&opt,
	)
	if err.Err() != nil {
		return err.Err()
	}
	return nil
}
