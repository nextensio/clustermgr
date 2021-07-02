package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strings"
)

func GetApodConnectService(namespace string, gateway string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/nextensio_connect_apod.yaml")
	if err != nil {
		log.Fatal(err)
	}
	vservice := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(vservice, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(podRepl, gateway)

	return gwRepl
}

func GetCpodConnectService(namespace string, gateway string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/nextensio_connect_cpod.yaml")
	if err != nil {
		log.Fatal(err)
	}
	vservice := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(vservice, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(podRepl, gateway)

	return gwRepl
}

func GetNxtForApodService(namespace string, gateway string, podname string, hostname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/nextensio_for_apod.yaml")
	if err != nil {
		log.Fatal(err)
	}
	vservice := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(vservice, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reHost := regexp.MustCompile(`REPLACE_HOST_NAME`)
	hostRepl := reHost.ReplaceAllString(podRepl, hostname)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(hostRepl, gateway)

	return gwRepl
}

func GetNxtForCpodService(namespace string, gateway string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/nextensio_for_cpod.yaml")
	if err != nil {
		log.Fatal(err)
	}
	vservice := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(vservice, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(podRepl, gateway)

	return gwRepl
}

func GetApodOutService(namespace string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/service_apod_out.yaml")
	if err != nil {
		log.Fatal(err)
	}
	service := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(service, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)

	return podRepl
}

func GetApodInService(namespace string, podname string, hostname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/service_apod_in.yaml")
	if err != nil {
		log.Fatal(err)
	}
	service := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(service, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reHost := regexp.MustCompile(`REPLACE_HOST_NAME`)
	hostRepl := reHost.ReplaceAllString(podRepl, hostname)

	return hostRepl
}

func GetCpodOutService(namespace string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/service_cpod_out.yaml")
	if err != nil {
		log.Fatal(err)
	}
	service := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(service, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)

	return podRepl
}

func GetCpodInService(namespace string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/service_cpod_in.yaml")
	if err != nil {
		log.Fatal(err)
	}
	service := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(service, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)

	return podRepl
}

func GetIngressGw(gateway string) string {
	content, err := ioutil.ReadFile(MyYaml + "/ingress_gw.yaml")
	if err != nil {
		log.Fatal(err)
	}
	ingressGw := string(content)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(ingressGw, gateway)
	return gwRepl
}

func GetEgressGw(gateway string) string {
	content, err := ioutil.ReadFile(MyYaml + "/egress_gw.yaml")
	if err != nil {
		log.Fatal(err)
	}
	svc := strings.Replace(gateway, ".", "-", -1)
	egressGw := string(content)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(egressGw, gateway)
	reSvc := regexp.MustCompile(`REPLACE_SVC_NAME`)
	svcRepl := reSvc.ReplaceAllString(gwRepl, svc)
	return svcRepl
}

func GetEgressGwDst(gateway string) string {
	content, err := ioutil.ReadFile(MyYaml + "/egress_gw_dest.yaml")
	if err != nil {
		log.Fatal(err)
	}
	svc := strings.Replace(gateway, ".", "-", -1)
	dest := string(content)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(dest, gateway)
	reSvc := regexp.MustCompile(`REPLACE_SVC_NAME`)
	svcRepl := reSvc.ReplaceAllString(gwRepl, svc)
	return svcRepl
}

func GetExtSvc(gateway string) string {
	content, err := ioutil.ReadFile(MyYaml + "/ext_svc.yaml")
	if err != nil {
		log.Fatal(err)
	}
	svc := strings.Replace(gateway, ".", "-", -1)
	extSvc := string(content)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(extSvc, gateway)
	reSvc := regexp.MustCompile(`REPLACE_SVC_NAME`)
	svcRepl := reSvc.ReplaceAllString(gwRepl, svc)
	return svcRepl
}

func GetApodDeploy(namespace string, image string, mongo string, podname string, cluster string, replicas int) string {
	content, err := ioutil.ReadFile(MyYaml + "/deploy_apod.yaml")
	if err != nil {
		log.Fatal(err)
	}
	deploy := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(deploy, namespace)
	reImg := regexp.MustCompile(`REPLACE_IMAGE`)
	deplRepl := reImg.ReplaceAllString(nspcRepl, image)
	reMongo := regexp.MustCompile(`REPLACE_MONGO`)
	mongoRepl := reMongo.ReplaceAllString(deplRepl, mongo)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(mongoRepl, podname)
	reClu := regexp.MustCompile(`REPLACE_CLUSTER`)
	cluRepl := reClu.ReplaceAllString(podRepl, cluster)
	reRepl := regexp.MustCompile(`REPLACE_REPLICAS`)
	replRepl := reRepl.ReplaceAllString(cluRepl, fmt.Sprintf("%d", replicas))

	return replRepl
}

func GetCpodDeploy(namespace string, image string, mongo string, podname string, cluster string, replicas int) string {
	content, err := ioutil.ReadFile(MyYaml + "/deploy_cpod.yaml")
	if err != nil {
		log.Fatal(err)
	}
	deploy := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(deploy, namespace)
	reImg := regexp.MustCompile(`REPLACE_IMAGE`)
	deplRepl := reImg.ReplaceAllString(nspcRepl, image)
	reMongo := regexp.MustCompile(`REPLACE_MONGO`)
	mongoRepl := reMongo.ReplaceAllString(deplRepl, mongo)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(mongoRepl, podname)
	reClu := regexp.MustCompile(`REPLACE_CLUSTER`)
	cluRepl := reClu.ReplaceAllString(podRepl, cluster)
	reRepl := regexp.MustCompile(`REPLACE_REPLICAS`)
	replRepl := reRepl.ReplaceAllString(cluRepl, fmt.Sprintf("%d", replicas))

	return replRepl
}

func GetConsul(myip string, storage string, cluster string) string {
	content, err := ioutil.ReadFile(MyYaml + "/consul.yaml")
	if err != nil {
		log.Fatal(err)
	}
	consul := string(content)
	reCsl := regexp.MustCompile(`REPLACE_SELF_NODE_IP`)
	csRepl := reCsl.ReplaceAllString(consul, myip)
	reClus := regexp.MustCompile(`REPLACE_CLUSTER`)
	clusRepl := reClus.ReplaceAllString(csRepl, cluster)
	reStorage := regexp.MustCompile(`REPLACE_STORAGE`)
	storageRepl := reStorage.ReplaceAllString(clusRepl, storage)

	return storageRepl
}

func GetFlowControl(namespace string) string {
	content, err := ioutil.ReadFile(MyYaml + "/flow_control.yaml")
	if err != nil {
		log.Fatal(err)
	}
	fc := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(fc, namespace)

	return nspcRepl
}

func GetFlowControlIstio() string {
	content, err := ioutil.ReadFile(MyYaml + "/istio_flow_control.yaml")
	if err != nil {
		log.Fatal(err)
	}
	fc := string(content)

	return fc
}

func GetOutlier(namespace string) string {
	content, err := ioutil.ReadFile(MyYaml + "/outlier_config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	fc := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(fc, namespace)

	return nspcRepl
}
