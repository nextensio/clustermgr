package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	common "gitlab.com/nextensio/common/go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/golang/glog"
)

var dbConnected bool
var unitTesting bool
var MyCluster string
var MyYaml string
var ConsulWanIP string
var ConsulStorage string
var MyMongo string
var MyJaeger string

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

// The error list is organized as a list of errors per tenant OR
// a list of errors per gateway - there is only those two classes of
// errors today, of counse there can be more in future, in which case
// see comments in DBErrToKey() for more info.
type ErrStack []*ErrRec

var errRecList map[string]*ErrStack
var eLock sync.RWMutex

func PushErr(v *ErrRec) {
	key := DBErrToKey(v)
	if key == "" {
		glog.Errorf("Bad key %v", v)
		return
	}
	stack := errRecList[key]
	if stack == nil {
		var s ErrStack
		s = append(s, v)
		errRecList[key] = &s
	} else {
		s := append(*stack, v)
		errRecList[key] = &s
	}
}

func DelErr(key string, i int) {
	s := errRecList[key]
	s1 := append((*s)[:i], (*s)[i+1:]...)
	errRecList[key] = &s1
}

func chopPath(orig string) string {
	ind := strings.LastIndex(orig, "/")
	if ind == -1 {
		return orig
	} else {
		return orig[ind+1:]
	}
}

func fnLine() string {
	function, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%s:%d", chopPath(file), runtime.FuncForPC(function).Name(), line)
}

// It helps to maintain the order in which errors happened. For example if we get a
// connector delete followed by a tenant delete, and lets say both failed - and now if
// we add it to an un-ordered error list, then we can attempt a tenant delete before
// a connector delete, and thats gonna fail (tenant has to be empty to be deleted). So
// we need to retry connector delete first and then the tenant delete
func errRetryProcess() {
	// After restart, clustermgr will rebuild everything from scratch, so
	// no need to retain error databse
	errRecCltn.Drop(context.TODO())

	for {
		eLock.Lock()
		for key, stack := range errRecList {
			if stack == nil {
				continue
			}
			var arrDelIdx []int
			for index, s := range *stack {
				var err error
				errMsg := fnLine()
				var clcfg *ClusterConfig
				glog.Infof("ErrorRetry: %s, %v", key, *s)
				switch s.Collection {
				case "NxtTenants":
					err, clcfg = DBFindTenantInCluster(s.Tenant)
					switch s.Operation {
					case "insert":
						if err == nil {
							errMsg, err = addNewTenant(clcfg)
						}
					case "delete":
						if err == nil {
							errMsg, err = deleteNamespace(s.Tenant, tenants[s.Tenant])
						}
					case "update":
						if err == nil {
							errMsg, err = updateAgents(clcfg)
						}
					}
				case "NxtConnectors":
					err, clcfg = DBFindTenantInCluster(s.Tenant)
					switch s.Operation {
					case "insert":
						if err == nil {
							errMsg, err = createConnectors(clcfg)
						}
					case "delete":
						if err == nil {
							errMsg, err = deleteConnector(s.Tenant, s.Connectid)
						}
					case "update":
						if err == nil {
							errMsg, err = createConnectors(clcfg)
						}
					}
				case "NxtGateways":
					// TODO: Gateways don't handle delete as of today
					errMsg, err = createEgressGateways()
				}
				if err != nil {
					s.Error = errMsg
					// We store minimal info in the database just to indicate to whoever
					// wants to know (controller ?) that there was some error processing
					// configs for this tenant. We "can" store a lot more detailed info here
					// but thats pbbly of no use because if there is an error like this, an
					// engineer has to be involved anyways, not much customer can do by seeing
					// the error details on controller
					DBAddErrRec(s)
					glog.Info("ErrorRetry failed")
				} else {
					// Can't delete entries in array while processing the array in loop so,
					// save the index to be deleted after the loop is processed
					arrDelIdx = append(arrDelIdx, index)
				}
			}
			if len(arrDelIdx) > 0 {
				// Now call the DelErr in reverse to avoid index
				for idx := range arrDelIdx {
					idx = len(arrDelIdx) - 1 - idx
					// now k starts from the end
					DelErr(key, arrDelIdx[idx])
				}
			}
		}
		eLock.Unlock()
		time.Sleep(2 * time.Second)
	}
}

func dumpErrors() {
	eLock.Lock()
	for key, stack := range errRecList {
		if stack == nil {
			continue
		}
		glog.Infof("dumpErrors: key %s, errors %d", key, len(*stack))
		for _, s := range *stack {
			glog.Infof("dumpErrors: %v", *s)
		}
	}
	eLock.Unlock()
}

func addError(err error, errMsg string, op string, collection string, tenant string, connector string) {
	if err == nil {
		return
	}
	timenow := fmt.Sprintf("%s", time.Now().Format(time.RFC1123))
	errRec := ErrRec{
		Tenant: tenant, Operation: op, Collection: collection,
		Error: errMsg + ":" + err.Error(), Connectid: connector, ChangeAt: timenow,
	}
	eLock.Lock()
	PushErr(&errRec)
	eLock.Unlock()
}

func watchClusterDB(cDB *mongo.Database) {
	var cs *mongo.ChangeStream
	var err error

	// Watch the cluster db. Retry for 5 times before bailing out of watch
	for retries := 5; retries > 0; retries-- {
		cs, err = cDB.Watch(context.TODO(), mongo.Pipeline{}, options.ChangeStream().SetFullDocument(options.UpdateLookup))
		if err != nil {
			// Call fatal only after the last retry otherwise, report error on continue retyring
			if retries <= 1 {
				glog.Fatalf("Not able to watch MongoDB Change notification-[err:%s]", err)
			} else {
				glog.Errorf("Not able to watch MongoDB Change notification-[err:%s] retrying... ", err)
				time.Sleep(1 * time.Second)
			}
		} else {
			glog.Info("Database watch started ")
			break
		}
	}

	// Whenever there is a new change event, decode the event and process  it
	for cs.Next(context.TODO()) {
		var changeEvent bson.M

		err = cs.Decode(&changeEvent)
		if err != nil {
			glog.Fatal(err)
		}

		op := changeEvent["operationType"].(string)
		// Check to prevent panic error
		if op == "drop" || op == "dropDatabase" || op == "invalidate" {
			continue
		}
		ns := changeEvent["ns"].(primitive.M)
		coll := ns["coll"].(string)

		switch coll {
		case "NxtTenants":
			dKey := changeEvent["documentKey"].(primitive.M)
			tenant := dKey["_id"].(string)
			var clcfg *ClusterConfig
			var err error
			errMsg := fnLine()
			switch op {
			case "insert":
				err, clcfg = DBFindTenantInCluster(tenant)
				if err == nil {
					errMsg, err = addNewTenant(clcfg)
				}
			case "delete":
				errMsg, err = deleteNamespace(tenant, tenants[tenant])
			case "update":
				err, clcfg = DBFindTenantInCluster(tenant)
				if err == nil {
					errMsg, err = updateAgents(clcfg)
				}
			}
			addError(err, errMsg, op, coll, tenant, "")
			glog.Infof("%s Tenant - %s %v %v clcfg:%v", op, tenant, err, errMsg, clcfg)

		case "NxtConnectors":
			connector := ""
			tenant := ""
			var clcfg *ClusterConfig
			var err error
			errMsg := fnLine()
			for key, val := range changeEvent["documentKey"].(primitive.M) {
				if key == "_id" {
					split := strings.Split(val.(string), ":")
					tenant = split[0]
					connector = val.(string)
					break
				}
			}

			err, clcfg = DBFindTenantInCluster(tenant)
			if err == nil {
				switch op {
				case "insert":
					errMsg, err = createConnectors(clcfg)
				case "delete":
					errMsg, err = deleteConnector(tenant, connector)
				case "update":
					errMsg, err = createConnectors(clcfg)
				}
			}
			addError(err, errMsg, op, coll, tenant, connector)
			glog.Info(op, " connector - ", connector, " to tenant - ", tenant, " err:", err, " clcfg:", clcfg, " ", errMsg)

		case "NxtGateways":
			// TODO: Gateways don't handle delete as of today
			errMsg, err := createEgressGateways()
			addError(err, errMsg, op, coll, "", "")
			glog.Info("Egress Gateway - ", op, err, errMsg)
		}
		if err = cs.Err(); err != nil {
			glog.Fatalf("Watch MongoDB Change notification disconnected. %v", err)
		}
	}
}

func addNewTenant(clcfg *ClusterConfig) (string, error) {
	var errMsg string
	errMsg, err := createTenants(clcfg)
	if err != nil {
		return errMsg, err
	}
	// If connectors are already configured in the cluster, we need to create the connectors when the tenant
	// is added to the cluster
	errMsg, err = createConnectors(clcfg)
	if err != nil {
		return errMsg, err
	}
	// Create the Egress gateways for the new tenant
	errMsg, err = createEgressGateways()
	if err != nil {
		return errMsg, err
	}

	return errMsg, nil
}

func deleteConnector(tenant string, id string) (string, error) {
	t := tenants[tenant]
	if t == nil {
		return fnLine(), errors.New("Tenant not found")
	}
	for i, c := range t.tenantSummary.Connectors {
		if c.Id == id {
			// First delete from kubectl and THEN update the summary database that then
			// entry has been deleted, so that if we crash in the midst of a delete, we
			// will still continue attempting a delete when we come back up next time.
			// Trying to delete non existant stuff will return a NotFound and we handle that
			// gracefully
			errMsg, err := deleteOneConnector(tenant, c.Connectid, &c)
			if err != nil {
				return errMsg, err
			}
			l := len(t.tenantSummary.Connectors) - 1
			t.tenantSummary.Connectors[i] = t.tenantSummary.Connectors[l]
			t.tenantSummary.Connectors = t.tenantSummary.Connectors[0:l]
			err = DBUpdateTenantSummary(tenant, t.tenantSummary)
			if err != nil {
				// put it back and try again next time
				t.tenantSummary.Connectors = append(t.tenantSummary.Connectors, c)
				return fnLine(), err
			}
			delete(t.bundleInfo, c.Connectid)
			break
		}
	}
	return "", nil
}

func updateAgents(clcfg *ClusterConfig) (string, error) {
	t := tenants[clcfg.Tenant]
	errMsg, err := createAgentDeployments(clcfg)
	if err != nil {
		return errMsg, err
	}
	t.deployVersion = clcfg.Version
	return "", nil
}

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

func checkKubeHardErr(errStr string) {
	if strings.Contains(errStr, "The connection to the server") ||
		strings.Contains(errStr, "You must be logged in to the server") ||
		strings.Contains(errStr, "error loading config file") {
		glog.Infof("Kube hard error encountered...")
		for { // Sit in a loop until the error is cleared
			cmd := exec.Command("kubectl", "get", "pod")
			_, err := cmd.CombinedOutput()
			if err == nil {
				glog.Infof("kube hard error -- cleared")
				break
			}
			time.Sleep(2 * time.Second)
		}
	}
}

func kubectlApply(file string) error {
	if unitTesting {
		kubeErr := GetEnv("TEST_KUBE_ERR", "NOT_TEST")
		if kubeErr == "true" {
			glog.Error("KubeApply UT error")
			return errors.New("Kubernetes unit test error")
		}
		return nil
	}
	cmd := exec.Command("kubectl", "apply", "-f", file)
	out, err := cmd.CombinedOutput()
	glog.Error("kubectl apply ", file, " result: ", string(out))
	if err != nil {
		checkKubeHardErr(string(out))
		glog.Error("kubectl apply ", file, " failed: ", string(out), " error: ", err)
		return err
	}

	return nil
}

func kubectlDelete(file string) (string, error) {
	if unitTesting {
		kubeErr := GetEnv("TEST_KUBE_ERR", "NOT_TEST")
		if kubeErr == "true" {
			glog.Error("KubeDelete UT error")
			return "", errors.New("Kubernetes unit test error")
		}
		return "", nil
	}
	cmd := exec.Command("kubectl", "delete", "-f", file)
	out, err := cmd.CombinedOutput()
	glog.Error("kubectl delete ", file, " result: ", string(out))
	if err != nil {
		checkKubeHardErr(string(out))
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

// Generate envoy flow control settings per tenant
func generateTenantFlowControl(t string) string {
	file := "/tmp/" + t + "/flow_control.yaml"
	yaml := GetFlowControl(t)
	return yamlFile(file, yaml)
}

// Generate route-reflector yaml for the  tenant
func generateTenantRouteReflector(t string) string {
	file := "/tmp/" + t + "/route_reflector.yaml"
	yaml := GetRouteReflector(t, MyCluster, MyMongo)
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
	yaml := GetApodDeploy(tenant, image, MyMongo, MyJaeger, podname, MyCluster, replicas)
	return yamlFile(file, yaml)
}

// Generate StatefulSet deployment for Cpod
func generateCpodDeploy(tenant string, image string, podname string, replicas int) string {
	file := "/tmp/" + tenant + "/deploy-" + podname + ".yaml"
	yaml := GetCpodDeploy(tenant, image, MyMongo, MyJaeger, podname, MyCluster, replicas)
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

// Generate envoy flow control settings per tenant
func generateApodHeadless(tenant string, podname string) string {
	file := "/tmp/" + tenant + "/headless-" + podname + ".yaml"
	yaml := GetApodHeadless(tenant, podname)
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

func createAgentDeployments(ct *ClusterConfig) (string, error) {
	t := tenants[ct.Tenant]
	summary := t.tenantSummary

	// Delete not-needed resources first before appying the new resources
	for i := 1; i <= ct.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		err := deleteApodService(ct.Tenant, podname, ct.ApodRepl, summary.ApodRepl, false)
		if err != nil {
			return fnLine(), err
		}
		err = deleteNxtForApod(ct.Tenant, podname, ct.ApodRepl, summary.ApodRepl)
		if err != nil {
			return fnLine(), err
		}
	}
	for i := ct.ApodSets + 1; i <= summary.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		err := deleteApodService(ct.Tenant, podname, 0, summary.ApodRepl, true)
		if err != nil {
			return fnLine(), err
		}
		err = deleteNxtForApod(ct.Tenant, podname, 0, summary.ApodRepl)
		if err != nil {
			return fnLine(), err
		}
		err = deleteApodNxtConnect(ct.Tenant, podname)
		if err != nil {
			return fnLine(), err
		}
		file := generateApodHeadless(ct.Tenant, podname)
		if file == "" {
			return fnLine(), errors.New("cannot create headless file")
		}
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		out, err := kubectlDelete(file)
		if err != nil && !strings.Contains(out, "NotFound") {
			return fnLine(), err
		}
		os.Remove(file)
		file = generateApodDeploy(ct.Tenant, summary.Image, podname, summary.ApodRepl)
		if file == "" {
			return fnLine(), errors.New("yaml fail")
		}
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		out, err = kubectlDelete(file)
		if err != nil && !strings.Contains(out, "NotFound") {
			return fnLine(), err
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
		return fnLine(), err
	}

	for i := 1; i <= ct.ApodSets; i++ {
		podname := getApodSetName(ct.Tenant, i)
		file := generateApodDeploy(ct.Tenant, ct.Image, podname, ct.ApodRepl)
		if file == "" {
			return fnLine(), errors.New("yaml fail")
		}
		err := kubectlApply(file)
		if err != nil {
			return fnLine(), err
		}
		err = createApodService(ct.Tenant, podname, ct.ApodRepl)
		if err != nil {
			return fnLine(), err
		}
		err = createNxtForApod(ct.Tenant, podname, ct.ApodRepl)
		if err != nil {
			return fnLine(), err
		}
		err = createApodNxtConnect(ct.Tenant, podname)
		if err != nil {
			return fnLine(), err
		}
		file = generateApodHeadless(ct.Tenant, podname)
		if file == "" {
			return fnLine(), errors.New("cannot create headless file")
		}
		err = kubectlApply(file)
		if err != nil {
			return fnLine(), err
		}
	}

	return "", nil
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
		checkKubeHardErr(string(out))
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
	// Recursively remove files in the directory
	for index := range dirFiles {
		fileHere := dirFiles[index]
		nameHere := fileHere.Name()
		fullPath := directory + "/" + nameHere
		os.Remove(fullPath)
	}
	// Now remove the directory
	os.Remove(directory)
}

func deleteNamespace(ns string, t *tenantInfo) (string, error) {
	var outs string
	var err error
	if len(t.tenantSummary.Connectors) != 0 || len(t.bundleInfo) != 0 {
		glog.Infof("Delete Namespace: connectors:%d   bundleInfo:%d", len(t.tenantSummary.Connectors), len(t.bundleInfo))
		return fnLine(), errors.New("Tenant still has bundles: " + ns)
	}
	for i := 1; i <= t.tenantSummary.ApodSets; i++ {
		podname := getApodSetName(ns, i)
		err = deleteApodService(ns, podname, 0, t.tenantSummary.ApodRepl, true)
		if err != nil {
			return fnLine(), err
		}
		file := generateApodHeadless(ns, podname)
		if file == "" {
			return fnLine(), errors.New("cannot create headless file")
		}
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		outs, err := kubectlDelete(file)
		if err != nil && !strings.Contains(outs, "NotFound") {
			return fnLine(), err
		}
		file = generateApodDeploy(ns, t.tenantSummary.Image, podname, t.tenantSummary.ApodRepl)
		if file == "" {
			return fnLine(), errors.New("yaml fail")
		}
		// clustermgr might have crashed while in here and come back up and now
		// we might be trying to delete something thats already deleted, so dont
		// panic in that case
		outs, err = kubectlDelete(file)
		if err != nil && !strings.Contains(outs, "NotFound") {
			return fnLine(), err
		}
	}

	file := generateTenantFlowControl(ns)
	if file == "" {
		return fnLine(), errors.New("yaml fail")
	}
	// clustermgr might have crashed while in here and come back up and now
	// we might be trying to delete something thats already deleted, so dont
	// panic in that case
	outs, err = kubectlDelete(file)
	if err != nil && !strings.Contains(outs, "NotFound") {
		return fnLine(), err
	}

	file, err = generateDockerCred(ns)
	if err != nil {
		return fnLine(), err
	}
	// clustermgr might have crashed while in here and come back up and now
	// we might be trying to delete something thats already deleted, so dont
	// panic in that case
	outs, err = kubectlDelete(file)
	if err != nil && !strings.Contains(outs, "NotFound") {
		return fnLine(), err
	}

	file = generateTenantRouteReflector(ns)
	if file == "" {
		return fnLine(), errors.New("yaml fail")
	}
	// clustermgr might have crashed while in here and come back up and now
	// we might be trying to delete something thats already deleted, so dont
	// panic in that case
	outs, err = kubectlDelete(file)
	if err != nil && !strings.Contains(outs, "NotFound") {
		return fnLine(), err
	}

	cmd := exec.Command("kubectl", "delete", "namespace", common.TenantToNamespace(ns))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "NotFound") {
			checkKubeHardErr(string(out))
			glog.Error("Cannot delete namespace ", ns, ": ", outs)
			return fnLine(), err
		}
	}
	err = DBDeleteTenantSummary(ns)
	if err != nil {
		return fnLine(), err
	}
	removeDir("/tmp/" + ns)
	delete(tenants, ns)
	return "", nil
}

func createNamespace(ns string) (string, error) {
	cmd := exec.Command("kubectl", "create", "namespace", common.TenantToNamespace(ns))
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "AlreadyExists") {
			checkKubeHardErr(string(out))
			glog.Error("Cannot create namespace ", ns, ": ", outs)
			return fnLine(), err
		}
	}
	cmd = exec.Command("kubectl", "label", "namespace", common.TenantToNamespace(ns), "istio-injection=enabled", "--overwrite")
	out, err = cmd.CombinedOutput()
	if err != nil {
		checkKubeHardErr(string(out))
		glog.Error("Cannot enable istio injection for namespace ", ns, ": ", string(out))
		return fnLine(), err
	}

	file := generateTenantFlowControl(ns)
	if file == "" {
		return fnLine(), errors.New("yaml fail")
	}
	err = kubectlApply(file)
	if err != nil {
		return fnLine(), err
	}

	file, err = generateDockerCred(ns)
	if err != nil {
		return fnLine(), err
	}
	err = kubectlApply(file)
	if err != nil {
		return fnLine(), err
	}

	file = generateTenantRouteReflector(ns)
	if file == "" {
		return fnLine(), errors.New("yaml fail")
	}
	err = kubectlApply(file)
	if err != nil {
		return fnLine(), err
	}

	return "", nil
}

func createTenants(clcfg *ClusterConfig) (string, error) {
	t := tenants[clcfg.Tenant]
	if t == nil || !t.created {
		if t == nil {
			tenants[clcfg.Tenant] = makeTenantInfo(clcfg.Tenant)
			t = tenants[clcfg.Tenant]
		}
		// Unknown tenant, so create tenant dir, then namespace.
		_ = os.Mkdir("/tmp/"+clcfg.Tenant, 0777)
		errMsg, err := createNamespace(clcfg.Tenant)
		if err != nil {
			return errMsg, err
		}
		t.created = true
	}
	t.markSweep = true

	if t.deployVersion != clcfg.Version {
		errMsg, err := createAgentDeployments(clcfg)
		if err != nil {
			return errMsg, err
		}
		t.deployVersion = clcfg.Version
	}
	return "", nil
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

func createEgressGws(gw string) (string, error) {
	err := createEgressGw(gw)
	if err != nil {
		return fnLine(), err
	}
	err = createExtsvc(gw)
	if err != nil {
		return fnLine(), err
	}
	err = createEgressGwDest(gw)
	if err != nil {
		return fnLine(), err
	}

	return "", nil
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

// Enable connections to other clusters via egress-gateways, etc.
// TODO: Deletion of remote gateways also needs to be handled, the
// added information needs to go into summary database to help with
// deletion
func createEgressGateways() (string, error) {
	err, cl := DBFindGatewayCluster(getGwName(MyCluster))
	if err != nil {
		return fnLine(), err
	}
	if cl == nil {
		return fnLine(), errors.New(fmt.Sprintf("Cant find my cluster : %s ", MyCluster))
	}
	if eGwVersion == cl.Version {
		return "", nil
	}
	for _, r := range cl.Remotes {
		errMsg, e := createEgressGws(getGwName(r))
		if e != nil {
			return errMsg, e
		}
	}
	eGwVersion = cl.Version
	return "", nil
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
		return "", fnLine(), errors.New("yaml fail")
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
		return "", fnLine(), errors.New("yaml fail")
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
		return "", fnLine(), errors.New("yaml fail")
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
		return "", fnLine(), errors.New("yaml fail")
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

func createOneConnector(b ClusterBundle, ct *ClusterConfig) (string, error) {
	file := generateCpodDeploy(ct.Tenant, ct.Image, b.Connectid, b.CpodRepl)
	if file == "" {
		glog.Error("Cpod deploy file failed", ct.Tenant, b.Connectid)
		return fnLine(), errors.New("Cannot create bundle file")
	}
	err := kubectlApply(file)
	if err != nil {
		glog.Error("Cpod deploy apply failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	err = createCpodOutService(ct.Tenant, b.Connectid)
	if err != nil {
		glog.Error("Cpod service failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	err = createCpodInService(ct.Tenant, b.Connectid)
	if err != nil {
		glog.Error("Cpod service failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	err = createCpodServiceReplica(ct.Tenant, b.Connectid, b.CpodRepl)
	if err != nil {
		glog.Error("Cpod service replica failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	if err := createCpodNxtFor(b); err != nil {
		glog.Error("Cpod for failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	if err := createNxtForCpodReplica(ct.Tenant, b.Connectid, b.CpodRepl); err != nil {
		glog.Error("Cpod for replica failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	if err := createCpodNxtConnect(b); err != nil {
		glog.Error("Cpod connect failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	file = generateCpodHealth(ct.Tenant, b.Connectid)
	if file == "" {
		glog.Error("Pod health file failed", ct.Tenant, b.Connectid)
		return fnLine(), errors.New("Cannot create health file")
	}
	err = kubectlApply(file)
	if err != nil {
		glog.Error("Pod health apply failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}
	file = generateCpodHeadless(ct.Tenant, b.Connectid)
	if file == "" {
		glog.Error("Pod headless file failed", ct.Tenant, b.Connectid)
		return fnLine(), errors.New("Cannot create headless file")
	}
	err = kubectlApply(file)
	if err != nil {
		glog.Error("Pod headless apply failed", err, ct.Tenant, b.Connectid)
		return fnLine(), err
	}

	return "", nil
}

// clustermgr might have crashed while in here and come back up and now
// we might be trying to delete something thats already deleted, so dont
// panic incase kubectl delete returns a "NotFound" error
func deleteOneConnector(tenant string, connectid string, c *ConnectorSummary) (string, error) {
	file, out, err := deleteCpodNxtFor(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod for failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)
	err = deleteNxtForCpodReplica(tenant, connectid, 0, c.CpodRepl)
	if err != nil {
		glog.Error("Cpod nxtfor delete replicas failed", err, tenant, connectid, c.CpodRepl)
		return fnLine(), err
	}
	file, out, err = deleteCpodNxtConnect(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod connect failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)
	file, out, err = deleteCpodOutService(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod service failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)
	file, out, err = deleteCpodInService(tenant, connectid)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod service failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)
	err = deleteCpodServiceReplica(tenant, connectid, 0, c.CpodRepl)
	if err != nil {
		glog.Error("Cpod service delete replicas failed", err, tenant, connectid, c.CpodRepl)
		return fnLine(), err
	}
	file = generateCpodHealth(tenant, connectid)
	if file == "" {
		glog.Error("Pod health file failed", tenant, connectid)
		return fnLine(), errors.New("Cannot create health file")
	}
	out, err = kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Pod health delete failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)
	file = generateCpodHeadless(tenant, connectid)
	if file == "" {
		glog.Error("Pod headless file failed", tenant, connectid)
		return fnLine(), errors.New("Cannot create health file")
	}
	out, err = kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Pod headless delete failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)
	file = generateCpodDeploy(tenant, c.Image, connectid, c.CpodRepl)
	if file == "" {
		glog.Error("Cpod deploy file failed", tenant, connectid)
		return fnLine(), errors.New("Cannot create bundle file")
	}
	out, err = kubectlDelete(file)
	if err != nil && !strings.Contains(out, "NotFound") {
		glog.Error("Cpod deploy delete failed", err, tenant, connectid)
		return fnLine(), err
	}
	os.Remove(file)

	return "", nil
}

func createConnectors(ct *ClusterConfig) (string, error) {
	var errMsg string
	if ct == nil {
		return fnLine(), errors.New("Tenant not found")
	}
	t := tenants[ct.Tenant]
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
		return fnLine(), err
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
			sumIdx := -1
			for i, c := range t.tenantSummary.Connectors {
				if c.Connectid == b.Connectid {
					sumIdx = i
					break
				}
			}
			if sumIdx == -1 {
				s := ConnectorSummary{Id: b.Uid, Image: ct.Image, Connectid: b.Connectid, CpodRepl: b.CpodRepl}
				t.tenantSummary.Connectors = append(t.tenantSummary.Connectors, s)
				sumIdx = len(t.tenantSummary.Connectors) - 1
			}
			summary := &t.tenantSummary.Connectors[sumIdx]
			// First remove resources thats not needed anymore. If the number of cpod
			// replicas hae reduced, we have to cleanup nxt-for and service rules etc..
			err := deleteNxtForCpodReplica(ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
			if err != nil {
				glog.Error("Cpod nxtfor delete replicas failed", ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
				return fnLine(), err
			}
			err = deleteCpodServiceReplica(ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
			if err != nil {
				glog.Error("Cpod service delete replicas failed", ct.Tenant, b.Connectid, b.CpodRepl, summary.CpodRepl)
				return fnLine(), err
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
				return fnLine(), err
			}
			errMsg, err = createOneConnector(b, ct)
			if err != nil {
				return errMsg, err
			}
			binfo.version = b.Version
			glog.Info("Cpod success ", ct.Tenant, b.Connectid)
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
			errMsg, err = deleteOneConnector(ct.Tenant, c.Connectid, &c)
			if err != nil {
				return errMsg, err
			}
			l := len(t.tenantSummary.Connectors) - 1
			t.tenantSummary.Connectors[i] = t.tenantSummary.Connectors[l]
			t.tenantSummary.Connectors = t.tenantSummary.Connectors[0:l]
			err = DBUpdateTenantSummary(ct.Tenant, t.tenantSummary)
			if err != nil {
				// put it back and try again next time
				t.tenantSummary.Connectors = append(t.tenantSummary.Connectors, c)
				return fnLine(), err
			}
			delete(t.bundleInfo, c.Connectid)
		}
	}

	return "", nil
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
	MyJaeger = GetEnv("MY_JAEGER_COLLECTOR", "UNKNOWN_JAEGER")
	if MyJaeger == "UNKNOWN_JAEGER" {
		glog.Fatal("Unknown Jaeger URI")
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
	errRecList = make(map[string]*ErrStack)

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
				// Copy s to tSum var so that we can assign the address to tenantSummary as s's addr
				// doesn't change in the for loop
				tSum := s
				_ = os.Mkdir("/tmp/"+s.Tenant, 0777)
				tenants[s.Tenant] = makeTenantInfo(tSum.Tenant)
				tenants[s.Tenant].tenantSummary = &tSum
			}
			break
		}
		glog.Error("Waiting to load configured tenants", err)
		time.Sleep(1 * time.Second)
	}

	for {
		err := createIngressGateway()
		if err == nil {
			break
		}
		glog.Error("Create ingress gw failed", err)
		time.Sleep(time.Second)
	}
	for {
		_, err := createEgressGateways()
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}

	// Do a mark and sweep of tenants if the tenant hasn't been removed properly
	for _, t := range tenants {
		t.markSweep = false
		// createTenants below will set it to true for tenants that still exist
	}
	for {
		err, clTcfg := DBFindAllTenantsInCluster()
		for _, Tcfg := range clTcfg {
			glog.Infof("Tenants in  %v:- <%v>", MyCluster, Tcfg.Tenant)
			for {
				_, err := createTenants(&Tcfg)
				if err == nil {
					break
				}
				glog.Error("Cannot create tenant", err)
				time.Sleep(time.Second)
			}
			for {
				_, err := createConnectors(&Tcfg)
				if err == nil {
					break
				}
				glog.Error("Cannot create connector", err)
				time.Sleep(time.Second)
			}
		}
		if err == nil {
			break
		}
		glog.Error("Cannot find tenants", err)
		time.Sleep(time.Second)
	}
	for k, t := range tenants {
		// If its still marked as false, then there is no such tenant
		if !t.markSweep {
			for {
				_, err := deleteNamespace(k, t)
				if err == nil {
					break
				}
				glog.Error("Cannot delete namespace", err)
			}
		}
	}

	// After we have run through the entire database once above,
	// register Cluster database for event notification and start event
	// based actions beyond this point
	go watchClusterDB(clusterDB)
	go errRetryProcess()

	// Do kill -USR1 <pid of mel> to get debugging info
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGUSR1)
	for {
		<-sigc
		dumpErrors()
	}
}

func main() {
	melMain()
}
