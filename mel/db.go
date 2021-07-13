package main

import (
	"context"

	"github.com/golang/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var dbClient *mongo.Client

// Collections for global operational info - clusters/gateways and tenants
var globalclusterDB *mongo.Database
var nxtGwCltn *mongo.Collection
var namespaceCltn *mongo.Collection
var clusterCfgCltn *mongo.Collection

// Collections specific to this cluster for tracking users and services
var clusterDB *mongo.Database
var usersCltn *mongo.Collection
var bundleCltn *mongo.Collection
var summaryCltn *mongo.Collection

func ClusterGetDBName(cl string) string {
	return ("Nxt-" + cl + "-DB")
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
	globalclusterDB = dbClient.Database("NxtClusterDB")
	nxtGwCltn = globalclusterDB.Collection("NxtGateways")
	namespaceCltn = globalclusterDB.Collection("NxtNamespaces")
	clusterCfgCltn = globalclusterDB.Collection("NxtClusters")

	clusterDB = dbClient.Database(ClusterGetDBName(MyCluster))
	usersCltn = clusterDB.Collection("NxtUsers")
	bundleCltn = clusterDB.Collection("NxtConnectors")
	summaryCltn = clusterDB.Collection("NxtTenantSummary")

	return true
}

type ConnectorSummary struct {
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

func DBFindAllTenantSummary() ([]TenantSummary, error) {
	var summary []TenantSummary

	cursor, err := summaryCltn.Find(context.TODO(), bson.M{})
	if err != nil {
		return summary, err
	}
	err = cursor.All(context.TODO(), &summary)
	if err != nil {
		return summary, err
	}

	return summary, nil
}

func DBFindTenantSummary(tenant string) *TenantSummary {
	var summary TenantSummary

	err := summaryCltn.FindOne(
		context.TODO(),
		bson.M{"_id": tenant},
	).Decode(&summary)
	if err != nil {
		return nil
	}
	return &summary
}

func DBUpdateTenantSummary(tenant string, summary *TenantSummary) error {

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
type NxtGateway struct {
	Name    string `json:"name" bson:"name"`
	Cluster string `json:"cluster" bson:"cluster"`
	Version int    `json:"version" bson:"version"`
}

func DBFindAllGateways() []NxtGateway {
	var gateways []NxtGateway

	cursor, err := nxtGwCltn.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &gateways)
	if err != nil {
		return nil
	}

	return gateways
}

// NOTE: The bson decoder will not work if the structure field names dont start with upper case
type Namespace struct {
	ID       string `json:"_id" bson:"_id"`
	Name     string `json:"name" bson:"name"`
	Database string `json:"database" bson:"database"`
	Version  int    `json:"version" bson:"version"`
}

func DBFindNamespace(id string) *Namespace {
	var namespace Namespace
	err := namespaceCltn.FindOne(
		context.TODO(),
		bson.M{"_id": id},
	).Decode(&namespace)
	if err != nil {
		return nil
	}
	return &namespace
}

func DBFindAllNamespaces() []Namespace {
	var namespaces []Namespace

	cursor, err := namespaceCltn.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &namespaces)
	if err != nil {
		return nil
	}

	return namespaces
}

type ClusterConfig struct {
	Id       string `json:"id" bson:"_id"` // ClusterID:TenantID
	Cluster  string `json:"cluster" bson:"cluster"`
	Tenant   string `json:"tenant" bson:"tenant"`
	Image    string `json:"image" bson:"image"`
	ApodRepl int    `json:"apodrepl" bson:"apodrepl"`
	ApodSets int    `json:"apodsets" bson:"apodsets"`
	Version  int    `json:"version" bson:"version"`
}

// Find all tenants present in a cluster
func DBFindAllTenantsInCluster(clid string) []ClusterConfig {
	var clcfg []ClusterConfig
	cursor, err := clusterCfgCltn.Find(context.TODO(), bson.M{"cluster": clid})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &clcfg)
	if err != nil {
		return nil
	}
	if len(clcfg) > 0 {
		return clcfg
	}
	return nil
}

// Find all clusters for specified tenant
func DBFindAllClustersForTenant(tenant string) []ClusterConfig {
	var clcfg []ClusterConfig
	cursor, err := clusterCfgCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &clcfg)
	if err != nil {
		return nil
	}
	if len(clcfg) > 0 {
		return clcfg
	}
	return nil
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
func DBFindClusterBundle(tenant string, bundleid string) *ClusterBundle {
	bid := tenant + ":" + bundleid
	var bundle ClusterBundle
	err := bundleCltn.FindOne(
		context.TODO(),
		bson.M{"_id": bid},
	).Decode(&bundle)
	if err != nil {
		return nil
	}
	return &bundle
}

func DBFindAllClusterBundlesForTenant(tenant string) []ClusterBundle {
	var bundles []ClusterBundle

	cursor, err := bundleCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &bundles)
	if err != nil {
		return nil
	}

	return bundles
}
