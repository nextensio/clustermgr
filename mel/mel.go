package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/golang/glog"
)

var MyCluster string
var MyYaml string
var ConsulWanIP string
var ConsulStorage string
var MyMongo string

var gwVersion map[string]int
var nspscVersion map[string]int
var deployVersion map[string]int
var apodForConnectVersion map[string]int
var bundleVersion map[string]int
var clusterMesh map[string]int

func GetEnv(key string, defaultValue string) string {
	v := os.Getenv(key)
	if v == "" {
		v = defaultValue
	}
	return v
}

func getApodSetName(tenant string, pod int) string {
	return tenant + "-apod" + fmt.Sprintf("%d", pod)
}

func getGwName(cluster string) string {
	return cluster + ".nextensio.net"
}

func kubectlApply(file string) error {
	cmd := exec.Command("kubectl", "apply", "-f", file)
	out, err := cmd.CombinedOutput()
	glog.Error("kubectl apply ", file, " result: ", string(out))
	if err != nil {
		glog.Error("kubectl apply ", file, " failed: ", string(out))
		return err
	}

	return nil
}

func kubectlDelete(file string) error {
	cmd := exec.Command("kubectl", "delete", "-f", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		glog.Error("kubectl delete ", file, " failed: ", string(out))
		return err
	}

	return nil
}

func yamlFile(file string, yaml string) string {
	f, err := os.Create(file)
	if err != nil {
		glog.Error("Cannot open yaml ", file)
		return ""
	}
	defer f.Close()
	_, err = io.WriteString(f, yaml)
	if err != nil {
		glog.Error("Cannot write yaml ", file)
		return ""
	}
	f.Sync()
	return file
}

// Generate envoy flow control settings for istio ingress/egress gw
func generateIstioFlowControl() string {
	file := "/tmp/istio_flow_control.yaml"
	yaml := GetFlowControl("istio-system")
	return yamlFile(file, yaml)
}

// Generate envoy flow control settings per tenant
func generateTenantFlowControl(t string) string {
	file := "/tmp/" + t + "/flow_control.yaml"
	yaml := GetFlowControl(t)
	return yamlFile(file, yaml)
}

// Generate virtual service to handle Cpod to Apod traffic based on x-nextensio-for
// header whose value is a pod name
func generateNxtForApod(t string, podname string, idx int) string {
	hostname := podname + fmt.Sprintf("-%d", idx)
	file := "/tmp/" + t + "/nxtfor-" + hostname + ".yaml"
	yaml := GetNxtForApodService(t, getGwName(MyCluster), podname, hostname)
	return yamlFile(file, yaml)
}

func createNxtForApod(t string, podname string, replicas int) error {
	var err error
	var file string
	for i := 0; i < replicas; i++ {
		// Repeat for each replica
		file = generateNxtForApod(t, podname, i)
		if file == "" {
			err = errors.New("yaml fail")
		} else {
			err1 := kubectlApply(file)
			if err1 != nil {
				err = err1
			}
		}
	}
	return err
}

// Generate virtual service to handle user connections into an Apod based
// on x-nextensio-connect header whose value is a pod name
func generateApodNxtConnect(t string, podname string) string {
	file := "/tmp/" + t + "/nxtconnect-" + podname + ".yaml"
	yaml := GetApodConnectService(t, getGwName(MyCluster), podname)
	return yamlFile(file, yaml)
}

func createApodNxtConnect(tenant string, podname string) error {
	file := generateApodNxtConnect(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	}
	return kubectlApply(file)
}

func createApodForConnect(ct *ClusterConfig) error {
	for i := 1; i <= ct.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		err := createNxtForApod(ct.Tenant, podname, ct.ApodRepl)
		if err != nil {
			return err
		}
		err = createApodNxtConnect(ct.Tenant, podname)
		if err != nil {
			return err
		}
	}
	return nil
}

// Generate StatefulSet deployment for Apod
func generateApodDeploy(ct *ClusterConfig, podname string, replicas int) string {
	file := "/tmp/" + ct.Tenant + "/deploy-" + podname + ".yaml"
	yaml := GetApodDeploy(ct.Tenant, ct.Image, MyMongo, podname, MyCluster, replicas)
	return yamlFile(file, yaml)
}

// Generate StatefulSet deployment for Cpod
func generateCpodDeploy(ct *ClusterConfig, podname string, replicas int) string {
	file := "/tmp/" + ct.Tenant + "/deploy-" + podname + ".yaml"
	yaml := GetCpodDeploy(ct.Tenant, ct.Image, MyMongo, podname, MyCluster, replicas)
	return yamlFile(file, yaml)
}

// Generate service for handling outside connections into an Apod
func generateApodOutService(tenant string, podname string) string {
	file := "/tmp/" + tenant + "/service-outside-" + podname + ".yaml"
	yaml := GetApodOutService(tenant, podname)
	return yamlFile(file, yaml)
}

// Generate service for inter-cluster traffic coming into an Apod
func generateApodInService(tenant string, podname string, idx int) string {
	hostname := podname + fmt.Sprintf("-%d", idx)
	file := "/tmp/" + tenant + "/service-inside-" + hostname + ".yaml"
	yaml := GetApodInService(tenant, podname, hostname)
	return yamlFile(file, yaml)
}

// Generate service for  traffic coming into a Cpod from within the nextensio network
func generateCpodInService(tenant string, podname string) string {
	file := "/tmp/" + tenant + "/service-inside-" + podname + ".yaml"
	yaml := GetCpodInService(tenant, podname)
	return yamlFile(file, yaml)
}

// Generate service for  traffic coming into a Cpod from connectors
func generateCpodOutService(tenant string, podname string) string {
	file := "/tmp/" + tenant + "/service-outside-" + podname + ".yaml"
	yaml := GetCpodOutService(tenant, podname)
	return yamlFile(file, yaml)
}

func createApodService(tenant string, podname string, replicas int) error {
	var err error
	var file string
	file = generateApodOutService(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	}
	err = kubectlApply(file)

	for i := 0; i < replicas; i++ {
		// Repeat for each replica
		file = generateApodInService(tenant, podname, i)
		if file == "" {
			err = errors.New("yaml fail")
		} else {
			err1 := kubectlApply(file)
			if err1 != nil {
				err = err1
			}
		}
	}
	return err
}

func deleteApodService(tenant string, podname string, replicaStart int, outside bool) error {
	if outside {
		file := "/tmp/" + tenant + "/service-outside-" + podname + ".yaml"
		err := kubectlDelete(file)
		if err != nil {
			return nil
		}
		exec.Command("rm", file)
	}

	var err error = nil
	for i := replicaStart; err == nil; i++ {
		// Repeat for each replica
		hostname := podname + fmt.Sprintf("-%d", i)
		file := "/tmp/" + tenant + "/service-inside-" + hostname + ".yaml"
		err = kubectlDelete(file)
		if err != nil {
			return err
		}
		exec.Command("rm", file)
	}
	return nil
}

func createAgentDeployments(ct *ClusterConfig) error {
	for i := 1; i <= ct.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		file := generateApodDeploy(ct, podname, ct.ApodRepl)
		if file == "" {
			return errors.New("yaml fail")
		}
		err := kubectlApply(file)
		if err != nil {
			return err
		}
		err = createApodService(ct.Tenant, podname, ct.ApodRepl)
		if err != nil {
			return err
		}
		// We maybe modifying existing apods, maybe reducing the number of
		// replicas, in which case cleanup services for the extra ones. We will
		// break out of the loop wit an error if we run beyond the number of
		// replicas configured last time. This will get simplified once Liyakath
		// introduces mongo changeset notifications and then we will exactly
		// know how many replicas we had before
		err = deleteApodService(ct.Tenant, podname, ct.ApodRepl, false)
		if err != nil {
			// do nothing, apply the new yaml for this pod
		}
	}
	// Now try to delete the extra deployments if any. If there are no
	// extra deployments, there will be an error attempting to delete and
	// we will automatically break out of the loop
	var err error = nil
	for i := ct.ApodSets + 1; err == nil; i++ {
		podname := getApodSetName(ct.Tenant, i)
		err = deleteApodService(ct.Tenant, podname, 0, true)
		if err != nil {
			// do nothing, delete this pod
		}
		file := "/tmp/" + ct.Tenant + "/deploy-" + podname + ".yaml"
		err = kubectlDelete(file)
		if err != nil {
			break
		}
	}

	return nil
}

func createNamespace(ns string) error {
	cmd := exec.Command("kubectl", "create", "namespace", ns)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "AlreadyExists") {
			glog.Error("Cannot create namespace ", ns, ": ", outs)
			return err
		}
	}
	cmd = exec.Command("kubectl", "label", "namespace", ns, "istio-injection=enabled", "--overwrite")
	out, err = cmd.CombinedOutput()
	if err != nil {
		glog.Error("Cannot enable istio injection for namespace ", ns, ": ", string(out))
		return err
	}

	file := generateTenantFlowControl(ns)
	if file == "" {
		return errors.New("yaml fail")
	}
	err = kubectlApply(file)
	if err != nil {
		return err
	}

	// Copy the docker keys to the new namespace
	file = "/tmp/" + ns + "/regcred.yaml"
	cmd = exec.Command("kubectl", "get", "secret", "regcred", "--namespace=default", "-o", "yaml")
	out, err = cmd.CombinedOutput()
	if err != nil {
		glog.Error("Cannot read docker credentials", err.Error())
		return err
	}
	regcred := string(out)
	reNspc := regexp.MustCompile(`namespace: default`)
	nspcRepl := reNspc.ReplaceAllString(regcred, "namespace: "+ns)

	// Replace some junk lines to make it legit yaml
	re := regexp.MustCompile("(?m)[[:space:]]+(creationTimestamp:).*$")
	nspcRepl = re.ReplaceAllString(nspcRepl, "")
	re = regexp.MustCompile("(?m)[[:space:]]+(time:).*$")
	nspcRepl = re.ReplaceAllString(nspcRepl, "")
	re = regexp.MustCompile("(?m)[[:space:]]+(uid:).*$")
	nspcRepl = re.ReplaceAllString(nspcRepl, "")
	re = regexp.MustCompile("(?m)[[:space:]]+(resourceVersion:).*$")
	nspcRepl = re.ReplaceAllString(nspcRepl, "")

	if yamlFile(file, nspcRepl) == "" {
		return errors.New("yaml file")
	}
	err = kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

func createTenants(clcfg *ClusterConfig) {

	_, ok1 := nspscVersion[clcfg.Tenant]
	if !ok1 {
		// Unknown tenant, so create tenant dir, then namespace.
		_ = os.Mkdir("/tmp/"+clcfg.Tenant, 0666)
		if createNamespace(clcfg.Tenant) == nil {
			nspscVersion[clcfg.Tenant] = 1
		}
	}
	v, ok2 := deployVersion[clcfg.Tenant]
	if !ok2 || (v != clcfg.Version) {
		if createAgentDeployments(clcfg) == nil {
			deployVersion[clcfg.Tenant] = clcfg.Version
		}
	}
	v, ok2 = apodForConnectVersion[clcfg.Tenant]
	if !ok2 || (v != clcfg.Version) {
		if createApodForConnect(clcfg) == nil {
			apodForConnectVersion[clcfg.Tenant] = clcfg.Version
		}
	}
}

//---------------------------------------Consul------------------------------------

func generateConsul() string {
	yaml := GetConsul(ConsulWanIP, ConsulStorage, MyCluster)
	return yamlFile("/tmp/consul.yaml", yaml)
}

func createConsul() error {
	var file string
	cmd := exec.Command("kubectl", "create", "namespace", "consul-system")
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "AlreadyExists") {
			glog.Error("Cannot create consul namespace: ", outs)
			return err
		}
	}

	for {
		file = generateConsul()
		if file != "" {
			break
		}
		// Well, no other option than to retry
		time.Sleep(1 * time.Second)
	}
	err = kubectlApply(file)
	if err != nil {
		return err
	}
	return nil
}

//-----------------------------------Gateways--------------------------------------

func generateEgressGwDest(gateway string) string {
	file := "/tmp/egwdst-" + gateway + ".yaml"
	yaml := GetEgressGwDst(gateway)
	return yamlFile(file, yaml)
}

func createEgressGwDest(gateway string) error {
	file := generateEgressGwDest(gateway)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

func generateEgressGw(gateway string) string {
	file := "/tmp/egw-" + gateway + ".yaml"
	yaml := GetEgressGw(gateway)
	return yamlFile(file, yaml)
}

func createEgressGw(gateway string) error {
	file := generateEgressGw(gateway)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

func generateExtsvc(gateway string) string {
	file := "/tmp/extsvc-" + gateway + ".yaml"
	yaml := GetExtSvc(gateway)
	return yamlFile(file, yaml)
}

func createExtsvc(gateway string) error {
	file := generateExtsvc(gateway)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

func createEgressGws(gw string) error {
	err := createEgressGw(gw)
	if err != nil {
		return err
	}
	err = createExtsvc(gw)
	if err != nil {
		return err
	}
	err = createEgressGwDest(gw)
	if err != nil {
		return err
	}

	return nil
}

func generateIngressGw() string {
	file := "/tmp/igw.yaml"
	yaml := GetIngressGw(getGwName(MyCluster))
	return yamlFile(file, yaml)
}

func createIngressGw() error {
	file := generateIstioFlowControl()
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return err
	}

	file = generateIngressGw()
	if file == "" {
		return errors.New("yaml fail")
	}
	err = kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

// Find out all other clusters (gateways) this cluster needs to connect to.
// First get all tenants in this cluster.
// For each such tenant, get tenant's presence in all other clusters.
// Get merged set of all those clusters and return a filtered set of
// newly discovered clusters.
func generateClusterMesh() map[string]int {
	var clusterm = make(map[string]int)
	var clusterdel = make(map[string]int)

	clTcfg := DBFindAllTenantsInCluster(MyCluster)
	for _, clTdoc := range clTcfg {
		// For every tenant in this cluster, get all other clusters
		// where the tenant has presence so we can enable connectivity
		// to those clusters.
		clGcfg := DBFindAllClustersForTenant(clTdoc.Tenant)
		for _, clGdoc := range clGcfg {
			if clGdoc.Cluster == MyCluster {
				continue
			}
			_, ok := clusterMesh[clGdoc.Cluster]
			// Keep track of known and unknown/new clusters
			if !ok {
				clusterm[clGdoc.Cluster] = 1 // New
			} else {
				clusterm[clGdoc.Cluster] = 2 // Known
			}
		}
	}
	// Now figure out if any previously known clusters have gone away
	// from our mesh so we can do any needed cleanup.
	for cl, _ := range clusterMesh {
		_, ok := clusterm[cl]
		if !ok {
			// cluster no longer used by any tenant in this cluster
			clusterdel[cl] = 1
		}
	}
	if len(clusterdel) > 0 {
		// Clean up yamls and remove egress-gateway config for
		// gateways this cluster does not need to connect to any more.
		// TODO: figure out how to do this.
		for cl, _ := range clusterdel {
			delete(clusterMesh, cl)
			delete(gwVersion, cl)
		}
	}

	// Add any newly discovered clusters to clusterMesh and leave just the
	// new clusters in clusterm for further processing.
	for cl, val := range clusterm {
		if val == 1 { // New cluster
			clusterMesh[cl] = 1 // value is immaterial
		} else {
			delete(clusterm, cl)
		}
	}
	return clusterm
}

// Enable connections to other clusters via egress-gateways, etc.
func createEgressGateways() {
	newclusters := generateClusterMesh()
	for cl, _ := range newclusters {
		_, ok := gwVersion[cl]
		if !ok {
			if createEgressGws(getGwName(cl)) == nil {
				gwVersion[cl] = 1
			}
		}
	}
}

// Create ingress-gateway for our own cluster
func createIngressGateway() {
	_, ok := gwVersion[MyCluster]
	if !ok {
		if createIngressGw() == nil {
			gwVersion[MyCluster] = 1
		}
	}
}

//-----------------------------Connector connections into Nextensio------------------------

// Generate virtual service to handle user connections into a Cpod based
// on x-nextensio-connect header whose value is currently the connector name
func generateCpodNxtConnect(a ClusterBundle) string {
	file := "/tmp/" + a.Tenant + "/nxtconnect-" + a.Connectid + ".yaml"
	yaml := GetCpodConnectService(a.Tenant, getGwName(MyCluster), a.Connectid)
	return yamlFile(file, yaml)
}

func createCpodNxtConnect(a ClusterBundle) error {
	file := generateCpodNxtConnect(a)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

// Generate virtual service to handle user connections into a Cpod based
// on x-nextensio-for header whose value is currently the connector name
func generateCpodNxtFor(a ClusterBundle) string {
	file := "/tmp/" + a.Tenant + "/nxtfor-" + a.Connectid + ".yaml"
	yaml := GetNxtForCpodService(a.Tenant, getGwName(MyCluster), a.Connectid)
	return yamlFile(file, yaml)
}

func createCpodNxtFor(a ClusterBundle) error {
	file := generateCpodNxtFor(a)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

func createCpodInService(tenant string, podname string) error {
	var err error
	var file string

	file = generateCpodInService(tenant, podname)
	if file == "" {
		err = errors.New("yaml fail")
	} else {
		err1 := kubectlApply(file)
		if err1 != nil {
			err = err1
		}
	}
	return err
}

func createCpodOutService(tenant string, podname string) error {
	var err error
	var file string

	file = generateCpodOutService(tenant, podname)
	if file == "" {
		err = errors.New("yaml fail")
	} else {
		err1 := kubectlApply(file)
		if err1 != nil {
			err = err1
		}
	}
	return err
}

func createConnectors(ct *ClusterConfig) {
	bundles := DBFindAllClusterBundlesForTenant(ct.Tenant)
	for _, a := range bundles {
		v, ok := bundleVersion[a.Uid]
		if ok && v == a.Version {
			continue
		}
		file := generateCpodDeploy(ct, a.Connectid, a.CpodRepl)
		if file == "" {
			glog.Error("Cpod deploy file failed", ct.Tenant, a.Connectid)
			continue
		}
		err := kubectlApply(file)
		if err != nil {
			glog.Error("Cpod deploy apply failed", err, ct.Tenant, a.Connectid)
			continue
		}
		err = createCpodOutService(ct.Tenant, a.Connectid)
		if err != nil {
			glog.Error("Cpod service failed", err, ct.Tenant, a.Connectid)
			continue
		}
		err = createCpodInService(ct.Tenant, a.Connectid)
		if err != nil {
			glog.Error("Cpod service failed", err, ct.Tenant, a.Connectid)
			continue
		}
		if err := createCpodNxtFor(a); err != nil {
			glog.Error("Cpod for failed", err, ct.Tenant, a.Connectid)
			continue
		}
		if err := createCpodNxtConnect(a); err != nil {
			glog.Error("Cpod connect failed", err, ct.Tenant, a.Connectid)
			continue
		}
		bundleVersion[a.Uid] = a.Version
		glog.Error("Cpod success", ct.Tenant, a.Connectid)
	}
}

//--------------------------------------Main---------------------------------------

func main() {
	flag.Parse()
	MyCluster = GetEnv("MY_POD_CLUSTER", "UNKNOWN_CLUSTER")
	if MyCluster == "UNKNOWN_CLUSTER" {
		glog.Fatal("Uknown cluster name")
	}
	MyYaml = GetEnv("MY_YAML", "UNKNOWN_YAML")
	if MyYaml == "UNKNOWN_YAML" {
		glog.Fatal("Uknown Yaml location")
	}
	ConsulWanIP = GetEnv("CONSUL_WAN_IP", "UNKNOWN_WAN_IP")
	if ConsulWanIP == "UNKNOWN_WAN_IP" {
		glog.Fatal("Uknown WAN IP")
	}
	ConsulStorage = GetEnv("CONSUL_STORAGE_CLASS", "UNKNOWN_CLASS")
	if ConsulStorage == "UNKNOWN_CLASS" {
		glog.Fatal("Uknown Consul Storage Class")
	}
	MyMongo = GetEnv("MY_MONGO_URI", "UNKNOWN_MONGO")
	if MyMongo == "UNKNOWN_MONGO" {
		glog.Fatal("Unknown Mongo URI")
	}

	//TODO: These versions will go away once we move to mongodb changeset
	//notifications, this is a temporary poor man's hack to periodically poll
	//mongo and apply only the changed ones
	gwVersion = make(map[string]int)
	nspscVersion = make(map[string]int)
	deployVersion = make(map[string]int)
	apodForConnectVersion = make(map[string]int)
	bundleVersion = make(map[string]int)
	clusterMesh = make(map[string]int)

	// Create consul
	for {
		if createConsul() == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	for {
		if DBConnect() == true {
			break
		}
		time.Sleep(1 * time.Second)
	}

	//TODO: This for loop will go away once we register with mongo for change notifications
	for {
		createIngressGateway()
		createEgressGateways()

		clTcfg := DBFindAllTenantsInCluster(MyCluster)
		for _, Tcfg := range clTcfg {
			createTenants(&Tcfg)
			createConnectors(&Tcfg)
		}
		time.Sleep(1 * time.Second)
	}
}
