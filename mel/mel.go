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

	common "gitlab.com/nextensio/common/go"

	"github.com/golang/glog"
)

var dbConnected bool
var unitTesting bool
var MyCluster string
var MyYaml string
var ConsulWanIP string
var ConsulStorage string
var MyMongo string

type bundleInfo struct {
	version   int
	markSweep bool
}
type tenantInfo struct {
	created       bool
	markSweep     bool
	tenantSummary *TenantSummary
	deployVersion int
	bundleInfo    map[string]*bundleInfo
}

var tenants map[string]*tenantInfo
var inGwVersion bool
var eGwVersion int
var clusterMesh map[string]int

func makeTenantInfo(tenant string) *tenantInfo {
	t := tenantInfo{}
	t.tenantSummary = &TenantSummary{}
	t.created = false
	t.markSweep = true
	t.deployVersion = -1
	t.bundleInfo = make(map[string]*bundleInfo)
	return &t
}

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
	if unitTesting {
		kubeErr := GetEnv("TEST_KUBE_ERR", "NOT_TEST")
		if kubeErr == "true" {
			return errors.New("Kubernetes unit test error")
		}
		return nil
	}
	cmd := exec.Command("kubectl", "apply", "-f", file)
	out, err := cmd.CombinedOutput()
	glog.Error("kubectl apply ", file, " result: ", string(out))
	if err != nil {
		glog.Error("kubectl apply ", file, " failed: ", string(out), " error: ", err)
		return err
	}

	return nil
}

func kubectlDelete(file string) (string, error) {
	if unitTesting {
		kubeErr := GetEnv("TEST_KUBE_ERR", "NOT_TEST")
		if kubeErr == "true" {
			return "", errors.New("Kubernetes unit test error")
		}
		return "", nil
	}
	cmd := exec.Command("kubectl", "delete", "-f", file)
	out, err := cmd.CombinedOutput()
	glog.Error("kubectl delete ", file, " result: ", string(out))
	if err != nil {
		glog.Error("kubectl delete ", file, " failed: ", string(out), " error: ", err)
		return string(out), err
	}

	return "", nil
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
	yaml := GetFlowControlIstio()
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

func deleteNxtForApod(t string, podname string, replicaStart int, replicaEnd int) error {
	var file string
	for i := replicaStart; i < replicaEnd; i++ {
		// Repeat for each replica
		file = generateNxtForApod(t, podname, i)
		if file == "" {
			return errors.New("yaml fail")
		} else {
			out, err := kubectlDelete(file)
			if err != nil && !strings.Contains(out, "NotFound") {
				return err
			}
			os.Remove(file)
		}
	}
	return nil
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

func deleteApodNxtConnect(tenant string, podname string) error {
	file := generateApodNxtConnect(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	}
	out, err := kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		return err
	}
	os.Remove(file)
	return nil
}

// Generate StatefulSet deployment for Apod
func generateApodDeploy(tenant string, image string, podname string, replicas int) string {
	file := "/tmp/" + tenant + "/deploy-" + podname + ".yaml"
	yaml := GetApodDeploy(tenant, image, MyMongo, podname, MyCluster, replicas)
	return yamlFile(file, yaml)
}

// Generate StatefulSet deployment for Cpod
func generateCpodDeploy(tenant string, image string, podname string, replicas int) string {
	file := "/tmp/" + tenant + "/deploy-" + podname + ".yaml"
	yaml := GetCpodDeploy(tenant, image, MyMongo, podname, MyCluster, replicas)
	return yamlFile(file, yaml)
}

// Generate envoy flow control settings per tenant
func generateCpodHealth(tenant string, podname string) string {
	file := "/tmp/" + tenant + "/health-" + podname + ".yaml"
	yaml := GetCpodHealth(tenant, podname)
	return yamlFile(file, yaml)
}

// Generate envoy flow control settings per tenant
func generateCpodHeadless(tenant string, podname string) string {
	file := "/tmp/" + tenant + "/headless-" + podname + ".yaml"
	yaml := GetCpodHeadless(tenant, podname)
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

func deleteApodService(tenant string, podname string, replicaStart int, replicaEnd int, outside bool) error {
	if outside {
		file := generateCpodOutService(tenant, podname)
		if file == "" {
			return errors.New("yaml file")
		}
		out, err := kubectlDelete(file)
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		if err != nil && !strings.Contains(out, "NotFound") {
			return err
		}
		os.Remove(file)
	}

	for i := replicaStart; i < replicaEnd; i++ {
		// Repeat for each replica
		file := generateApodInService(tenant, podname, i)
		if file == "" {
			return errors.New("yaml file")
		}
		out, err := kubectlDelete(file)
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		if err != nil && !strings.Contains(out, "NotFound") {
			glog.Error("Inside service del failed,", i)
			return err
		}
		os.Remove(file)
	}
	return nil
}

func createAgentDeployments(ct *ClusterConfig) error {
	t := tenants[ct.Tenant]
	summary := t.tenantSummary

	// Delete not-needed resources first before appying the new resources
	for i := 1; i <= ct.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		err := deleteApodService(ct.Tenant, podname, ct.ApodRepl, summary.ApodRepl, false)
		if err != nil {
			return err
		}
		err = deleteNxtForApod(ct.Tenant, podname, ct.ApodRepl, summary.ApodRepl)
		if err != nil {
			return err
		}
	}
	for i := ct.ApodSets + 1; i <= summary.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		err := deleteApodService(ct.Tenant, podname, 0, summary.ApodRepl, true)
		if err != nil {
			return err
		}
		err = deleteNxtForApod(ct.Tenant, podname, 0, summary.ApodRepl)
		if err != nil {
			return err
		}
		err = deleteApodNxtConnect(ct.Tenant, podname)
		if err != nil {
			return err
		}
		file := generateApodDeploy(ct.Tenant, summary.Image, podname, summary.ApodRepl)
		if file == "" {
			return errors.New("yaml fail")
		}
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		out, err := kubectlDelete(file)
		if err != nil && !strings.Contains(out, "NotFound") {
			return err
		}
		os.Remove(file)
	}

	// Update the latest values first BEFORE trying to apply kubectl.
	// If we crash in the midst of applying kubectl, we need to have
	// the summary database reflect what we were attempting, a delete
	// using the unapplied values in summary will just say NotFound and
	// we handle that gracefully
	summary.Tenant = ct.Tenant
	summary.ApodRepl = ct.ApodRepl
	summary.ApodSets = ct.ApodSets
	summary.Image = ct.Image
	if err := DBUpdateTenantSummary(ct.Tenant, summary); err != nil {
		return err
	}

	for i := 1; i <= ct.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		file := generateApodDeploy(ct.Tenant, ct.Image, podname, ct.ApodRepl)
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
		err = createNxtForApod(ct.Tenant, podname, ct.ApodRepl)
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

func generateDockerCred(ns string) (string, error) {
	if unitTesting {
		return "/tmp/" + ns + "/regcred.yaml", nil
	}

	// Copy the docker keys to the new namespace
	file := "/tmp/" + ns + "/regcred.yaml"
	cmd := exec.Command("kubectl", "get", "secret", "regcred", "--namespace=default", "-o", "yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		glog.Error("Cannot read docker credentials", err.Error())
		return "", err
	}
	regcred := string(out)
	reNspc := regexp.MustCompile(`namespace: default`)
	nspcRepl := reNspc.ReplaceAllString(regcred, "namespace: "+common.TenantToNamespace(ns))

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
		return "", errors.New("yaml file")
	}

	return file, nil
}

func removeDir(directory string) {
	dirRead, _ := os.Open(directory)
	dirFiles, _ := dirRead.Readdir(0)
	for index := range dirFiles {
		fileHere := dirFiles[index]
		nameHere := fileHere.Name()
		fullPath := directory + "/" + nameHere
		os.Remove(fullPath)
	}
}

func deleteNamespace(ns string, t *tenantInfo) error {
	var outs string
	var err error
	if len(t.tenantSummary.Connectors) != 0 || len(t.bundleInfo) != 0 {
		return errors.New("Tenant still has bundles: " + ns)
	}
	for i := 1; i <= t.tenantSummary.ApodSets; i++ {
		podname := getApodSetName(ns, i)
		err = deleteApodService(ns, podname, 0, t.tenantSummary.ApodRepl, true)
		if err != nil {
			return err
		}
		file := generateApodDeploy(ns, t.tenantSummary.Image, podname, t.tenantSummary.ApodRepl)
		if file == "" {
			return errors.New("yaml fail")
		}
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		outs, err = kubectlDelete(file)
		if err != nil && !strings.Contains(outs, "NotFound") {
			return err
		}
	}

	file := generateTenantFlowControl(ns)
	if file == "" {
		return errors.New("yaml fail")
	}
	// clustermgr might have crashed while in here and come back up and now
	// we might be trying to delete something thats already deleted, so dont
	// panic in that case
	outs, err = kubectlDelete(file)
	if err != nil && !strings.Contains(outs, "NotFound") {
		return err
	}

	file, err = generateDockerCred(ns)
	if err != nil {
		return err
	}
	// clustermgr might have crashed while in here and come back up and now
	// we might be trying to delete something thats already deleted, so dont
	// panic in that case
	outs, err = kubectlDelete(file)
	if err != nil && !strings.Contains(outs, "NotFound") {
		return err
	}

	cmd := exec.Command("kubectl", "delete", "namespace", common.TenantToNamespace(ns))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "NotFound") {
			glog.Error("Cannot delete namespace ", ns, ": ", outs)
			return err
		}
	}
	err = DBDeleteTenantSummary(ns)
	if err != nil {
		return err
	}
	removeDir("/tmp/" + ns)
	delete(tenants, ns)
	return nil
}

func createNamespace(ns string) error {
	cmd := exec.Command("kubectl", "create", "namespace", common.TenantToNamespace(ns))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "AlreadyExists") {
			glog.Error("Cannot create namespace ", ns, ": ", outs)
			return err
		}
	}
	cmd = exec.Command("kubectl", "label", "namespace", common.TenantToNamespace(ns), "istio-injection=enabled", "--overwrite")
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

	file, err = generateDockerCred(ns)
	if err != nil {
		return err
	}
	err = kubectlApply(file)
	if err != nil {
		return err
	}

	return nil
}

func createTenants(clcfg *ClusterConfig) {
	t := tenants[clcfg.Tenant]
	if t == nil || !t.created {
		if t == nil {
			tenants[clcfg.Tenant] = makeTenantInfo(clcfg.Tenant)
			t = tenants[clcfg.Tenant]
		}
		// Unknown tenant, so create tenant dir, then namespace.
		_ = os.Mkdir("/tmp/"+clcfg.Tenant, 0777)
		if createNamespace(clcfg.Tenant) != nil {
			return
		}
		t.created = true
	}
	t.markSweep = true

	if t.deployVersion != clcfg.Version {
		if createAgentDeployments(clcfg) == nil {
			t.deployVersion = clcfg.Version
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

// Enable connections to other clusters via egress-gateways, etc.
// TODO: Deletion of remote gateways also needs to be handled, the
// added information needs to go into summary database to help with
// deletion
func createEgressGateways() error {
	err, cl := DBFindGatewayCluster(getGwName(MyCluster))
	if err != nil {
		return err
	}
	if cl == nil {
		return errors.New("Cant find my cluster")
	}
	if eGwVersion == cl.Version {
		return nil
	}
	for _, r := range cl.Remotes {
		e := createEgressGws(getGwName(r))
		if e != nil {
			return e
		}
	}
	eGwVersion = cl.Version

	return nil
}

// Create ingress-gateway for our own cluster
func createIngressGateway() error {
	if !inGwVersion {
		e := createIngressGw()
		if e != nil {
			return e
		}
		inGwVersion = true
	}
	return nil
}

//-----------------------------Connector connections into Nextensio------------------------

// Generate virtual service to handle user connections into a Cpod based
// on x-nextensio-connect header whose value is currently the connector name
func generateCpodNxtConnect(tenant string, connectid string) string {
	file := "/tmp/" + tenant + "/nxtconnect-" + connectid + ".yaml"
	yaml := GetCpodConnectService(tenant, getGwName(MyCluster), connectid)
	return yamlFile(file, yaml)
}

func deleteCpodNxtConnect(tenant string, connectid string) (string, string, error) {
	file := generateCpodNxtConnect(tenant, connectid)
	if file == "" {
		return "", "", errors.New("yaml fail")
	}
	out, err := kubectlDelete(file)
	return file, out, err
}

func createCpodNxtConnect(a ClusterBundle) error {
	file := generateCpodNxtConnect(a.Tenant, a.Connectid)
	if file == "" {
		return errors.New("yaml fail")
	}
	return kubectlApply(file)
}

// Generate virtual service to handle Cpod to Apod traffic based on x-nextensio-for
// header whose value is a pod name
func generateNxtForCpodReplica(t string, podname string, idx int) string {
	hostname := podname + fmt.Sprintf("-%d", idx)
	file := "/tmp/" + t + "/nxtfor-" + hostname + ".yaml"
	yaml := GetNxtForCpodServiceReplica(t, getGwName(MyCluster), podname, hostname)
	return yamlFile(file, yaml)
}

func createNxtForCpodReplica(t string, podname string, replicas int) error {
	var err error
	var file string
	for i := 0; i < replicas; i++ {
		// Repeat for each replica
		file = generateNxtForCpodReplica(t, podname, i)
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

func deleteNxtForCpodReplica(t string, podname string, replicaStart int, replicaEnd int) error {
	var file string
	for i := replicaStart; i < replicaEnd; i++ {
		// Repeat for each replica
		file = generateNxtForCpodReplica(t, podname, i)
		if file == "" {
			return errors.New("yaml fail")
		} else {
			out, err := kubectlDelete(file)
			if err != nil && !strings.Contains(out, "NotFound") {
				return err
			}
			os.Remove(file)
		}
	}
	return nil
}

// Generate virtual service to handle user connections into a Cpod based
// on x-nextensio-for header whose value is currently the connector name
func generateCpodNxtFor(tenant string, connectid string) string {
	file := "/tmp/" + tenant + "/nxtfor-" + connectid + ".yaml"
	yaml := GetNxtForCpodService(tenant, getGwName(MyCluster), connectid)
	return yamlFile(file, yaml)
}

func deleteCpodNxtFor(tenant string, connectid string) (string, string, error) {
	file := generateCpodNxtFor(tenant, connectid)
	if file == "" {
		return "", "", errors.New("yaml fail")
	}
	out, err := kubectlDelete(file)
	return file, out, err
}

func createCpodNxtFor(a ClusterBundle) error {
	file := generateCpodNxtFor(a.Tenant, a.Connectid)
	if file == "" {
		return errors.New("yaml fail")
	}
	return kubectlApply(file)
}

// Generate service for inter-cluster traffic coming into an Apod
func generateCpodInServiceReplica(tenant string, podname string, idx int) string {
	hostname := podname + fmt.Sprintf("-%d", idx)
	file := "/tmp/" + tenant + "/service-inside-" + hostname + ".yaml"
	yaml := GetCpodInServiceReplica(tenant, podname, hostname)
	return yamlFile(file, yaml)
}

func createCpodServiceReplica(tenant string, podname string, replicas int) error {
	for i := 0; i < replicas; i++ {
		// Repeat for each replica
		file := generateCpodInServiceReplica(tenant, podname, i)
		if file == "" {
			return errors.New("yaml fail")
		} else {
			err := kubectlApply(file)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteCpodServiceReplica(tenant string, podname string, replicaStart int, replicaEnd int) error {
	for i := replicaStart; i < replicaEnd; i++ {
		// Repeat for each replica
		file := generateCpodInServiceReplica(tenant, podname, i)
		if file == "" {
			return errors.New("yaml fail")
		}
		out, err := kubectlDelete(file)
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		if err != nil && !strings.Contains(out, "NotFound") {
			glog.Error("Inside service del failed,", i)
			return err
		}
		os.Remove(file)
	}
	return nil
}

func deleteCpodInService(tenant string, podname string) (string, string, error) {
	file := generateCpodInService(tenant, podname)
	if file == "" {
		return "", "", errors.New("yaml fail")
	} else {
		out, err := kubectlDelete(file)
		return file, out, err
	}
}

func createCpodInService(tenant string, podname string) error {
	file := generateCpodInService(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	} else {
		return kubectlApply(file)
	}
}

func deleteCpodOutService(tenant string, podname string) (string, string, error) {
	file := generateCpodOutService(tenant, podname)
	if file == "" {
		return "", "", errors.New("yaml fail")
	} else {
		out, err := kubectlDelete(file)
		return file, out, err
	}
}

func createCpodOutService(tenant string, podname string) error {
	file := generateCpodOutService(tenant, podname)
	if file == "" {
		return errors.New("yaml fail")
	} else {
		return kubectlApply(file)
	}
}

func createOneConnector(b ClusterBundle, ct *ClusterConfig) error {
	file := generateCpodDeploy(ct.Tenant, ct.Image, b.Connectid, b.CpodRepl)
	if file == "" {
		glog.Error("Cpod deploy file failed", ct.Tenant, b.Connectid)
		return errors.New("Cannot create bundle file")
	}
	err := kubectlApply(file)
	if err != nil {
		glog.Error("Cpod deploy apply failed", err, ct.Tenant, b.Connectid)
		return err
	}
	err = createCpodOutService(ct.Tenant, b.Connectid)
	if err != nil {
		glog.Error("Cpod service failed", err, ct.Tenant, b.Connectid)
		return err
	}
	err = createCpodInService(ct.Tenant, b.Connectid)
	if err != nil {
		glog.Error("Cpod service failed", err, ct.Tenant, b.Connectid)
		return err
	}
	err = createCpodServiceReplica(ct.Tenant, b.Connectid, b.CpodRepl)
	if err != nil {
		glog.Error("Cpod service replica failed", err, ct.Tenant, b.Connectid)
		return err
	}
	if err := createCpodNxtFor(b); err != nil {
		glog.Error("Cpod for failed", err, ct.Tenant, b.Connectid)
		return err
	}
	if err := createNxtForCpodReplica(ct.Tenant, b.Connectid, b.CpodRepl); err != nil {
		glog.Error("Cpod for replica failed", err, ct.Tenant, b.Connectid)
		return err
	}
	if err := createCpodNxtConnect(b); err != nil {
		glog.Error("Cpod connect failed", err, ct.Tenant, b.Connectid)
		return err
	}
	file = generateCpodHealth(ct.Tenant, b.Connectid)
	if file == "" {
		glog.Error("Pod health file failed", ct.Tenant, b.Connectid)
		return errors.New("Cannot create health file")
	}
	err = kubectlApply(file)
	if err != nil {
		glog.Error("Pod health apply failed", err, ct.Tenant, b.Connectid)
		return err
	}
	file = generateCpodHeadless(ct.Tenant, b.Connectid)
	if file == "" {
		glog.Error("Pod headless file failed", ct.Tenant, b.Connectid)
		return errors.New("Cannot create health file")
	}
	err = kubectlApply(file)
	if err != nil {
		glog.Error("Pod headless apply failed", err, ct.Tenant, b.Connectid)
		return err
	}

	return nil
}

// clustermgr might have crashed while in here and come back up and now
// we might be trying to delete something thats already deleted, so dont
// panic incase kubectl delete returns a "NotFound" error
func deleteOneConnector(tenant string, connectid string, c *ConnectorSummary) error {
	file, out, err := deleteCpodNxtFor(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod for failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)
	err = deleteNxtForCpodReplica(tenant, connectid, 0, c.CpodRepl)
	if err != nil {
		glog.Error("Cpod nxtfor delete replicas failed", err, tenant, connectid, c.CpodRepl)
		return err
	}
	file, out, err = deleteCpodNxtConnect(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod connect failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)
	file, out, err = deleteCpodOutService(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod service failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)
	file, out, err = deleteCpodInService(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod service failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)
	err = deleteCpodServiceReplica(tenant, connectid, 0, c.CpodRepl)
	if err != nil {
		glog.Error("Cpod service delete replicas failed", err, tenant, connectid, c.CpodRepl)
		return err
	}
	file = generateCpodHealth(tenant, connectid)
	if file == "" {
		glog.Error("Pod health file failed", tenant, connectid)
		return errors.New("Cannot create health file")
	}
	out, err = kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Pod health delete failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)
	file = generateCpodHeadless(tenant, connectid)
	if file == "" {
		glog.Error("Pod headless file failed", tenant, connectid)
		return errors.New("Cannot create health file")
	}
	out, err = kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Pod headless delete failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)
	file = generateCpodDeploy(tenant, c.Image, connectid, c.CpodRepl)
	if file == "" {
		glog.Error("Cpod deploy file failed", tenant, connectid)
		return errors.New("Cannot create bundle file")
	}
	out, err = kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod deploy apply failed", err, tenant, connectid)
		return err
	}
	os.Remove(file)

	return nil
}

func createConnectors(ct *ClusterConfig) error {
	t := tenants[ct.Tenant]
	// Till we have mongo notifications working, do a mark and sweep
	for _, c := range t.tenantSummary.Connectors {
		binfo := t.bundleInfo[c.Connectid]
		if binfo == nil {
			binfo = &bundleInfo{}
			binfo.version = -1
			t.bundleInfo[c.Connectid] = binfo
		}
		t.bundleInfo[c.Connectid].markSweep = false
	}

	err, bundles := DBFindAllClusterBundlesForTenant(ct.Tenant)
	if err != nil {
		return err
	}
	for _, b := range bundles {
		binfo := t.bundleInfo[b.Connectid]
		if binfo == nil {
			binfo = &bundleInfo{}
			binfo.version = -1
			t.bundleInfo[b.Connectid] = binfo
		}
		binfo.markSweep = true
		if binfo.version != b.Version {
			var summary *ConnectorSummary
			for _, c := range t.tenantSummary.Connectors {
				if c.Connectid == b.Connectid {
					summary = &c
					break
				}
			}
			if summary == nil {
				summary = &ConnectorSummary{Image: ct.Image, Connectid: b.Connectid, CpodRepl: b.CpodRepl}
				t.tenantSummary.Connectors = append(t.tenantSummary.Connectors, *summary)
			}
			// First remove resources thats not needed anymore. If the number of cpod
			// replicas hae reduced, we have to cleanup nxt-for and service rules etc..
			err := deleteNxtForCpodReplica(ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
			if err != nil {
				glog.Error("Cpod nxtfor delete replicas failed", ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
				return err
			}
			err = deleteCpodServiceReplica(ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
			if err != nil {
				glog.Error("Cpod service delete replicas failed", ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
				return err
			}
			summary.Image = ct.Image
			summary.CpodRepl = b.CpodRepl
			// Update the latest values first BEFORE trying to apply kubectl.
			// If we crash in the midst of applying kubectl, we need to have
			// the summary database reflect what we were attempting, a delete
			// using the unapplied values in summary will just say NotFound and
			// we handle that gracefully
			err = DBUpdateTenantSummary(ct.Tenant, t.tenantSummary)
			if err != nil {
				return err
			}
			err = createOneConnector(b, ct)
			if err != nil {
				return err
			}
			binfo.version = b.Version
			glog.Error("Cpod success", ct.Tenant, b.Connectid)
		}
	}

	// Till we have mongo notifications working, do a mark and sweep and delete bundles
	// that are still marked as false
	for i, c := range t.tenantSummary.Connectors {
		if !t.bundleInfo[c.Connectid].markSweep {
			// First delete from kubectl and THEN update the summary database that then
			// entry has been deleted, so that if we crash in the midst of a delete, we
			// will still continue attempting a delete when we come back up next time.
			// Trying to delete non existant stuff will return a NotFound and we handle that
			// gracefully
			err = deleteOneConnector(ct.Tenant, c.Connectid, &c)
			if err != nil {
				return err
			}
			l := len(t.tenantSummary.Connectors) - 1
			t.tenantSummary.Connectors[i] = t.tenantSummary.Connectors[l]
			t.tenantSummary.Connectors = t.tenantSummary.Connectors[0:l]
			err = DBUpdateTenantSummary(ct.Tenant, t.tenantSummary)
			if err != nil {
				// put it back and try again next time
				t.tenantSummary.Connectors = append(t.tenantSummary.Connectors, c)
				return err
			}
			delete(t.bundleInfo, c.Connectid)
		}
	}

	return nil
}

//--------------------------------------Main---------------------------------------

func melMain() {
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
	TestEnviron := GetEnv("TEST_ENVIRONMENT", "NOT_TEST")
	if TestEnviron == "true" {
		unitTesting = true
	}

	//TODO: These versions will go away once we move to mongodb changeset
	//notifications, this is a temporary poor man's hack to periodically poll
	//mongo and apply only the changed ones
	inGwVersion = false
	eGwVersion = 0
	tenants = make(map[string]*tenantInfo)
	clusterMesh = make(map[string]int)

	// Create consul
	for {
		if createConsul() == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	for {
		if DBConnect() {
			dbConnected = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Find the tenants that have been already configured
	for {
		err, summary := DBFindAllTenantSummary()
		if err == nil {
			for _, s := range summary {
				_ = os.Mkdir("/tmp/"+s.Tenant, 0777)
				tenants[s.Tenant] = makeTenantInfo(s.Tenant)
				tenants[s.Tenant].tenantSummary = &s
			}
			break
		}
		glog.Error("Waiting to load configured tenants", err)
		time.Sleep(1 * time.Second)
	}

	//TODO: This for loop will go away once we register with mongo for change notifications
	for {
		createIngressGateway()
		createEgressGateways()

		// Till we get the mongo notifications working, do a mark and sweep of tenants
		// to see who has been deleted etc..
		for _, t := range tenants {
			t.markSweep = false
			// createTenants below will set it to true for tenants that still exist
		}
		_, clTcfg := DBFindAllTenantsInCluster()
		for _, Tcfg := range clTcfg {
			createTenants(&Tcfg)
			createConnectors(&Tcfg)
		}
		for k, t := range tenants {
			// If its still marked as false, then there is no such tenant
			if !t.markSweep {
				deleteNamespace(k, t)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func main() {
	melMain()
}
