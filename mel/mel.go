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

var ConsulDNS string
var MyCluster string
var MyYaml string
var MyWanIP string
var MyMongo string

var gwVersion map[string]int
var nspscVersion map[string]int
var deployVersion map[string]int
var userVersion map[string]int
var bundleVersion map[string]int
var usvcVersion map[string]int
var bsvcVersion map[string]int
var clusterMesh map[string]int

func GetEnv(key string, defaultValue string) string {
	v := os.Getenv(key)
	if v == "" {
		v = defaultValue
	}
	return v
}

func getPodName(pod int, podtype string) string {
	prefix := "apod"
	if podtype != "A" {
		prefix = "cpod"
	}
	return prefix + fmt.Sprintf("%d", pod)
}

func getGwName(cluster string) string {
	return cluster + ".nextensio.net"
}

func kubectlApply(file string) error {
	cmd := exec.Command("kubectl", "apply", "-f", file)
	out, err := cmd.CombinedOutput()
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

// This is how the yamls are connected together:
// User agent sets apodname in x-nextensio-connect header -> matched by
// nxtconnect-<apodname> -> service-outside-<apodname> -> deploy-<apodname>
// Cpod sets target apodname in x-nextensio-for header -> matcheed by
// nxtfor-<apodname> -> service-inside-<apodname> -> deploy-<apodname>
//
// Connector agent sets connector name in x-nextensio-connect header -> matched by
// nxtconnect-<connectorname> -> service-outside-<cpodname> -> deploy-<cpodname>
// Apod sets target service name in x-nextensio-for header -> matched by
// nxtfor-<servicename> -> service-inside-<cpodname> -> deploy-<cpodname>
//
//-------------------------------Pod Deployment & Namespace-------------------------------

const apodReplicas = 2
const cpodReplicas = 1

// Generate virtual service to handle Cpod to Apod traffic based on x-nextensio-for
// header whose value is a pod name
func generateNxtForApod(t string, podname string, idx int) string {
	hostname := podname + fmt.Sprintf("-%d", idx)
	file := "/tmp/nxtfor-" + t + "-" + hostname + ".yaml"
	yaml := GetNxtForApodService(t, getGwName(MyCluster), podname, hostname)
	return yamlFile(file, yaml)
}

func createNxtForApod(t string, podname string) error {
	var err error
	var file string
	for i := 0; i < apodReplicas; i++ {
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
	file := "/tmp/nxtconnect-" + t + "-" + podname + ".yaml"
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

func createUserConnects(ct *ClusterConfig) error {
	for i := 1; i <= ct.Apods; i++ {
		podname := getPodName(i, "A")
		err := createNxtForApod(ct.Tenant, podname)
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
func generateApodDeploy(ct *ClusterConfig, podname string) string {
	file := "/tmp/deploy-" + ct.Tenant + "-" + podname + ".yaml"
	yaml := GetApodDeploy(ct.Tenant, ct.Image, MyMongo, podname, MyCluster, ConsulDNS)
	return yamlFile(file, yaml)
}

// Generate StatefulSet deployment for Cpod
func generateCpodDeploy(ct *ClusterConfig, podname string) string {
	file := "/tmp/deploy-" + ct.Tenant + "-" + podname + ".yaml"
	yaml := GetCpodDeploy(ct.Tenant, ct.Image, MyMongo, podname, MyCluster, ConsulDNS)
	return yamlFile(file, yaml)
}

// Generate service for handling outside connections into either an Apod
// or Cpod.
func generateOutsideService(tenant string, podname string) string {
	file := "/tmp/service-outside-" + tenant + "-" + podname + ".yaml"
	yaml := GetOutsideService(tenant, podname)
	return yamlFile(file, yaml)
}

// Generate service for inter-cluster traffic coming into an Apod
func generateApodInService(tenant string, podname string, idx int) string {
	hostname := podname + fmt.Sprintf("-%d", idx)
	file := "/tmp/service-inside-" + tenant + "-" + hostname + ".yaml"
	yaml := GetApodInService(tenant, podname, hostname)
	return yamlFile(file, yaml)
}

// Generate service for inter-cluster traffic coming into a Cpod
func generateCpodInService(tenant string, podname string) string {
	file := "/tmp/service-inside-" + tenant + "-" + podname + ".yaml"
	yaml := GetCpodInService(tenant, podname)
	return yamlFile(file, yaml)
}

func createApodService(tenant string, podname string) error {
	var err error
	var file string
	file = generateOutsideService(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	}
	err = kubectlApply(file)

	for i := 0; i < apodReplicas; i++ {
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

func deleteApodService(tenant string, podname string) error {
	file := "/tmp/service-outside-" + tenant + "-" + podname + ".yaml"
	err := kubectlDelete(file)
	if err != nil {
		return nil
	}

	for i := 0; i < apodReplicas; i++ {
		// Repeat for each replica
		hostname := podname + fmt.Sprintf("-%d", i)
		file = "/tmp/service-inside-" + tenant + "-" + hostname + ".yaml"
		err = kubectlDelete(file)
		if err != nil {
			return err
		}
	}
	return nil
}

func createCpodService(tenant string, podname string) error {
	var err error
	var file string
	file = generateOutsideService(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	}
	err = kubectlApply(file)

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

func deleteCpodService(tenant string, podname string) error {
	file := "/tmp/service-outside-" + tenant + "-" + podname + ".yaml"
	err := kubectlDelete(file)
	if err != nil {
		return err
	}
	file = "/tmp/service-inside-" + tenant + "-" + podname + ".yaml"
	err = kubectlDelete(file)
	if err != nil {
		return err
	}
	return nil
}

func createDeploy(ct *ClusterConfig) error {
	for i := 1; i <= ct.Apods; i++ {
		podname := getPodName(i, "A")
		file := generateApodDeploy(ct, podname)
		if file == "" {
			return errors.New("yaml fail")
		}
		err := kubectlApply(file)
		if err != nil {
			return err
		}
		err = createApodService(ct.Tenant, podname)
		if err != nil {
			return err
		}
	}
	// Now try to delete the extra deployments if any. If there are no
	// extra deployments, there will be an error attempting to delete and
	// we will automatically break out of the loop
	var err error = nil
	for i := ct.Apods + 1; err == nil; i++ {
		podname := getPodName(i, "A")
		err = deleteApodService(ct.Tenant, podname)
		if err != nil {
			break
		}
		file := "/tmp/deploy-" + ct.Tenant + "-" + podname + ".yaml"
		err = kubectlDelete(file)
		if err != nil {
			break
		}
	}
	for i := 1; i <= ct.Cpods; i++ {
		podname := getPodName(i, "C")
		file := generateCpodDeploy(ct, podname)
		if file == "" {
			return errors.New("yaml fail")
		}
		err := kubectlApply(file)
		if err != nil {
			return err
		}
		err = createCpodService(ct.Tenant, podname)
		if err != nil {
			return err
		}
	}
	// Now try to delete the extra deployments if any. If there are no
	// extra deployments, there will be an error attempting to delete and
	// we will automatically break out of the loop
	err = nil
	for i := ct.Cpods + 1; err == nil; i++ {
		podname := getPodName(i, "C")
		err = deleteCpodService(ct.Tenant, podname)
		if err != nil {
			break
		}
		file := "/tmp/deploy-" + ct.Tenant + "-" + podname + ".yaml"
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

	// Copy the docker keys to the new namespace
	file := "/tmp/" + ns + "-regcred.yaml"
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
		// Unknown tenant, so create namespace
		if createNamespace(clcfg.Tenant) == nil {
			nspscVersion[clcfg.Tenant] = 1
		}
	}
	v, ok2 := deployVersion[clcfg.Tenant]
	if !ok2 || (v != clcfg.Version) {
		if createDeploy(clcfg) == nil {
			deployVersion[clcfg.Tenant] = clcfg.Version
		}
	}
	v, ok2 = userVersion[clcfg.Tenant]
	if !ok2 || (v != clcfg.Version) {
		if createUserConnects(clcfg) == nil {
			userVersion[clcfg.Tenant] = clcfg.Version
		}
	}
}

//---------------------------------------Consul------------------------------------

func getConsulDNS() string {
	cmd := exec.Command("kubectl", "get", "svc", MyCluster+"-consul-dns", "-n", "consul-system", "-o", "jsonpath='{.spec.clusterIP}'")
	out, err := cmd.CombinedOutput()
	if err != nil {
		glog.Error("Cannot get consul DNS IP: ", string(out))
		return ""
	}
	return string(out)
}

func generateConsul() string {
	yaml := GetConsul(MyWanIP, MyCluster)
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
		return nil
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
	file := generateIngressGw()
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
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
func generateCpodNxtConnect(a ClusterUser) string {
	file := "/tmp/nxtconnect-" + a.Uid + ".yaml"
	podname := getPodName(a.Pod, "C")
	yaml := GetCpodConnectService(a.Tenant, getGwName(MyCluster), podname, a.Connectid)
	return yamlFile(file, yaml)
}

func createCpodNxtConnect(a ClusterUser) error {
	file := generateCpodNxtConnect(a)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return nil
	}

	return nil
}

func createAgents(tenant string) {
	agents := DBFindAllClusterBundlesForTenant(tenant)
	for _, a := range agents {
		v, ok := bundleVersion[a.Uid]
		if ok && v == a.Version {
			continue
		}
		if createCpodNxtConnect(a) == nil {
			bundleVersion[a.Uid] = a.Version
		}
	}
}

//-----------------------Apod to Cpod Inter-cluster connectivity-------------------------

// Generate virtual service to handle Apod to Cpod traffic based on the
// x-nextensio-for header whose value is a service name
func generateNxtForCpod(s ClusterService) string {
	if len(s.Agents) == 0 {
		return ""
	}
	tenant_svc := strings.Split(s.Sid, ":")
	svc := strings.ReplaceAll(s.Sid, "@", "-")
	svc = strings.ReplaceAll(svc, ".", "-")
	file := "/tmp/nxtfor-" + svc + ".yaml"
	//TODO: Today we handle only the case of one agent advertising a service,
	// when we have multiple agents for the same service, we need to modify the
	// yaml with some kind of loadbalancing across these agent pods etc..
	// For now, pick the first pod.
	podname := getPodName(s.Pods[0], "C")
	yaml := GetNxtForCpodService(s.Tenant, getGwName(MyCluster), podname, tenant_svc[1])
	return yamlFile(file, yaml)
}

func createNxtForCpod(s ClusterService) error {
	file := generateNxtForCpod(s)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return nil
	}

	return nil
}

func createServices(tenant string) {
	svcs := DBFindAllBundleClusterSvcsForTenant(tenant)
	for _, s := range svcs {
		v, ok := bsvcVersion[s.Sid]
		if ok && v == s.Version {
			continue
		}
		if createNxtForCpod(s) == nil {
			bsvcVersion[s.Sid] = s.Version
		}
	}
}

//--------------------------------------Main---------------------------------------

func main() {
	flag.Parse()
	// We were executed as command line yaml generator, nothing more to do after that
	if Cmdline() == true {
		return
	}
	MyCluster = GetEnv("MY_POD_CLUSTER", "UNKNOWN_CLUSTER")
	if MyCluster == "UNKNOWN_CLUSTER" {
		glog.Fatal("Uknown cluster name")
	}
	MyYaml = GetEnv("MY_YAML", "UNKNOWN_YAML")
	if MyYaml == "UNKNOWN_YAML" {
		glog.Fatal("Uknown Yaml location")
	}
	MyWanIP = GetEnv("MY_WAN_IP", "UNKNOWN_WAN_IP")
	if MyWanIP == "UNKNOWN_WAN_IP" {
		glog.Fatal("Uknown WAN IP")
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
	userVersion = make(map[string]int)
	bundleVersion = make(map[string]int)
	usvcVersion = make(map[string]int)
	bsvcVersion = make(map[string]int)
	clusterMesh = make(map[string]int)

	// Create consul
	for {
		if createConsul() == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	// Get the consul server dns IP address
	for {
		ConsulDNS = getConsulDNS()
		if ConsulDNS != "" {
			ConsulDNS = ConsulDNS[1 : len(ConsulDNS)-1]
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
			createAgents(Tcfg.Tenant)
			createServices(Tcfg.Tenant)
		}
		time.Sleep(1 * time.Second)
	}
}
