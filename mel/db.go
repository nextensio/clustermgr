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
var userviceCltn *mongo.Collection
var bserviceCltn *mongo.Collection

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
	userviceCltn = clusterDB.Collection("NxtUServices")
	bserviceCltn = clusterDB.Collection("NxtBServices")

	return true
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
	Image    string `json:"image" bson:"image"`
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
	Apods    int    `json:"apods" bson:"apods"`
	Cpods    int    `json:"cpods" bson:"cpods"`
	NextApod int    `json:"nextapod" bson:"nextapod"`
	NextCpod int    `json:"nextcpod" bson:"nextcpod"`
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

type ClusterUser struct {
	Uid       string   `json:"uid" bson:"_id"`
	Tenant    string   `json:"tenant" bson:"tenant"`
	Pod       int      `json:"pod" bson:"pod"`
	Connectid string   `json:"connectid" bson:"connectid"`
	Services  []string `json:"services" bson:"services"`
	Version   int      `json:"version" bson:"version"`
}

func DBFindClusterUser(tenant string, userid string) *ClusterUser {
	uid := tenant + ":" + userid
	var user ClusterUser
	err := usersCltn.FindOne(
		context.TODO(),
		bson.M{"_id": uid},
	).Decode(&user)
	if err != nil {
		return nil
	}
	return &user
}

func DBFindAllClusterUsersForTenant(tenant string) []ClusterUser {
	var users []ClusterUser

	cursor, err := usersCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &users)
	if err != nil {
		return nil
	}

	return users
}

// Find a specific tenant's connector within a cluster
func DBFindClusterBundle(tenant string, bundleid string) *ClusterUser {
	uid := tenant + ":" + bundleid
	var user ClusterUser
	err := bundleCltn.FindOne(
		context.TODO(),
		bson.M{"_id": uid},
	).Decode(&user)
	if err != nil {
		return nil
	}
	return &user
}

func DBFindAllClusterBundlesForTenant(tenant string) []ClusterUser {
	var users []ClusterUser

	cursor, err := bundleCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &users)
	if err != nil {
		return nil
	}

	return users
}

type ClusterService struct {
	Sid     string   `json:"sid" bson:"_id"`
	Tenant  string   `json:"tenant" bson:"tenant"`
	Agents  []string `json:"agents" bson:"agents"`
	Pods    []int    `json:"pods" bson:"pods"`
	Version int      `json:"version" bson:"version"`
}

// Find a specific tenant user service within a cluster
func DBFindUserClusterSvc(tenant string, service string) *ClusterService {
	sid := tenant + ":" + service
	var svc ClusterService
	err := userviceCltn.FindOne(
		context.TODO(),
		bson.M{"_id": sid},
	).Decode(&svc)
	if err != nil {
		return nil
	}
	return &svc
}

// Find all user services within a cluster for a specific tenant
func DBFindAllUserClusterSvcsForTenant(tenant string) []ClusterService {
	var svcs []ClusterService

	cursor, err := userviceCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &svcs)
	if err != nil {
		return nil
	}

	return svcs
}

// Find a specific tenant connector service within a cluster
func DBFindBundleClusterSvc(tenant string, service string) *ClusterService {
	sid := tenant + ":" + service
	var svc ClusterService
	err := bserviceCltn.FindOne(
		context.TODO(),
		bson.M{"_id": sid},
	).Decode(&svc)
	if err != nil {
		return nil
	}
	return &svc
}

// Find all connector services within a cluster for a specific tenant
func DBFindAllBundleClusterSvcsForTenant(tenant string) []ClusterService {
	var svcs []ClusterService

	cursor, err := bserviceCltn.Find(context.TODO(), bson.M{"tenant": tenant})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &svcs)
	if err != nil {
		return nil
	}

	return svcs
}
