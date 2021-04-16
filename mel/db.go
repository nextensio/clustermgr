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
var clusterDB *mongo.Database
var nxtGwCltn *mongo.Collection
var namespaceCltn *mongo.Collection
var usersCltn *mongo.Collection
var serviceCltn *mongo.Collection

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
		glog.Error("Database ping failed")
		return false
	}
	clusterDB = dbClient.Database("ClusterDB")
	nxtGwCltn = clusterDB.Collection("NxtGateways")
	namespaceCltn = clusterDB.Collection("NxtNamespaces")
	usersCltn = clusterDB.Collection("NxtUsers")
	serviceCltn = clusterDB.Collection("NxtServices")

	return true
}

// NOTE: The bson decoder will not work if the structure field names dont start with upper case
type NxtGateway struct {
	Name    string `json:"name" bson:"name"`
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
	Pods     int    `json:"pods" bson:"pods"`
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

func DBFindAllClusterUsers() []ClusterUser {
	var users []ClusterUser

	cursor, err := usersCltn.Find(context.TODO(), bson.M{})
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

func DBFindClusterSvc(tenant string, service string) *ClusterService {
	sid := tenant + ":" + service
	var svc ClusterService
	err := serviceCltn.FindOne(
		context.TODO(),
		bson.M{"_id": sid},
	).Decode(&svc)
	if err != nil {
		return nil
	}
	return &svc
}

func DBFindAllClusterSvcs() []ClusterService {
	var svcs []ClusterService

	cursor, err := serviceCltn.Find(context.TODO(), bson.M{})
	if err != nil {
		return nil
	}
	err = cursor.All(context.TODO(), &svcs)
	if err != nil {
		return nil
	}

	return svcs
}
