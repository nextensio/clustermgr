package main

import (
	"errors"
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
var agentVersion map[string]int
var svcVersion map[string]int

func GetEnv(key string, defaultValue string) string {
	v := os.Getenv(key)
	if v == "" {
		v = defaultValue
	}
	return v
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

func generateDeploy(n Namespace, podname string) string {
	file := "/tmp/deploy-" + n.ID.Hex() + "-" + podname + ".yaml"
	yaml := GetDeploy(n.ID.Hex(), n.Image, MyMongo, podname, MyCluster, ConsulDNS)
	return yamlFile(file, yaml)
}

func generateService(n Namespace, podname string) string {
	file := "/tmp/service-" + n.ID.Hex() + "-" + podname + ".yaml"
	yaml := GetService(n.ID.Hex(), podname)
	return yamlFile(file, yaml)
}

func createDeploy(n Namespace) error {
	for i := 1; i <= n.Pods; i++ {
		podname := fmt.Sprintf("pod%d", i)
		file := generateDeploy(n, podname)
		if file == "" {
			return errors.New("yaml fail")
		}
		err := kubectlApply(file)
		if err != nil {
			return err
		}
		file = generateService(n, podname)
		if file == "" {
			return errors.New("yaml fail")
		}
		err = kubectlApply(file)
		if err != nil {
			return err
		}
	}

	return nil
}

func createNamespace(n Namespace) error {
	cmd := exec.Command("kubectl", "create", "namespace", n.ID.Hex())
	out, err := cmd.CombinedOutput()
	if err != nil {
		outs := string(out)
		if !strings.Contains(outs, "AlreadyExists") {
			glog.Error("Cannot create namespace ", n.ID.Hex(), ": ", outs)
			return err
		}
	}
	cmd = exec.Command("kubectl", "label", "namespace", n.ID.Hex(), "istio-injection=enabled", "--overwrite")
	out, err = cmd.CombinedOutput()
	if err != nil {
		glog.Error("Cannot enable istio injection for namespace ", n.ID.Hex(), ": ", string(out))
		return err
	}

	// Copy the docker keys to the new namespace
	file := "/tmp/" + n.ID.Hex() + "-regcred.yaml"
	cmd = exec.Command("kubectl", "get", "secret", "regcred", "--namespace=default", "-o", "yaml")
	out, err = cmd.CombinedOutput()
	if err != nil {
		glog.Error("Cannot read docker credentials", err.Error())
		return err
	}
	regcred := string(out)
	reNspc := regexp.MustCompile(`namespace: default`)
	nspcRepl := reNspc.ReplaceAllString(regcred, "namespace: "+n.ID.Hex())

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
	yaml := GetIngressGw(MyCluster + ".nextensio.net")
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

func createTenants() {
	namespaces := DBFindAllNamespaces()

	for _, n := range namespaces {
		v, ok := nspscVersion[n.ID.Hex()]
		if ok && v == n.Version {
			continue
		}
		nspscVersion[n.ID.Hex()] = n.Version

		for {
			if createNamespace(n) == nil {
				break
			}
			// There is no difference between namespaces, if creating one fails, then the next
			// will fail too, so we ensure this one succeeds before proceeding
			time.Sleep(1 * time.Second)
		}
		for {
			if createDeploy(n) == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func generateNxtConnect(a ClusterUser) string {
	file := "/tmp/nxtconnect-" + a.Uid + ".yaml"
	podname := fmt.Sprintf("pod%d", a.Pod)
	yaml := GetAgentVservice(a.Tenant.Hex(), MyCluster+".nextensio.net", podname, a.Connectid)
	return yamlFile(file, yaml)
}

func createNxtConnect(a ClusterUser) error {
	file := generateNxtConnect(a)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return nil
	}

	return nil
}

func generateNxtFor(s ClusterService) string {
	if len(s.Agents) == 0 {
		return ""
	}
	file := "/tmp/nxtfor-" + s.Sid + ".yaml"
	//TODO: Today we handle only the case of one agent advertising a service, when we have multiple
	// agents for the same service, we need to modify the yaml with some kind of loadbalancing across
	// these agent pods etc..
	podname := fmt.Sprintf("pod%d", s.Pods[0])
	tenant_svc := strings.Split(s.Sid, ":")
	yaml := GetAppVservice(s.Tenant.Hex(), MyCluster+".nextensio.net", podname, tenant_svc[1])
	return yamlFile(file, yaml)
}

func createNxtFor(s ClusterService) error {
	file := generateNxtFor(s)
	if file == "" {
		return errors.New("yaml fail")
	}
	err := kubectlApply(file)
	if err != nil {
		return nil
	}

	return nil
}

func createAgents() {
	agents := DBFindAllClusterUsers()

	for _, a := range agents {
		v, ok := agentVersion[a.Uid]
		if ok && v == a.Version {
			continue
		}
		agentVersion[a.Uid] = a.Version

		tenant := DBFindNamespace(a.Tenant)
		if tenant == nil {
			glog.Error("User ", a.Uid, a.Tenant, " without parent tenant")
			continue
		}
		for {
			if createNxtConnect(a) == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func createServices() {
	svcs := DBFindAllClusterSvcs()

	for _, s := range svcs {
		v, ok := svcVersion[s.Sid]
		if ok && v == s.Version {
			continue
		}
		svcVersion[s.Sid] = s.Version

		tenant := DBFindNamespace(s.Tenant)
		if tenant == nil {
			glog.Error("Service ", s.Sid, " without parent tenant")
			continue
		}

		for {
			if createNxtFor(s) == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func createGateways() {
	gateways := DBFindAllGateways()
	for _, gw := range gateways {
		v, ok := gwVersion[gw.Name]
		if ok && v == gw.Version {
			continue
		}
		gwVersion[gw.Name] = gw.Version

		if strings.Contains(gw.Name, MyCluster+".nextensio.net") {
			for {
				if createIngressGw() == nil {
					break
				}
				time.Sleep(1 * time.Second)
			}
		} else {
			for {
				if createEgressGws(gw.Name) == nil {
					break
				}
				time.Sleep(1 * time.Second)
			}
		}
	}
}

func main() {
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
	agentVersion = make(map[string]int)
	svcVersion = make(map[string]int)

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
		createGateways()
		createTenants()
		createAgents()
		createServices()
		time.Sleep(5 * time.Second)
	}
}
