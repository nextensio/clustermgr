package main

import (
	"io/ioutil"
	"log"
	"regexp"
	"strings"
)

func GetAgentVservice(namespace string, gateway string, podname string, agent string, utype string) string {
	var yamltemplate string
	if utype == "A" {
		yamltemplate = "/nextensio_connect_user.yaml"
	} else {
		yamltemplate = "/nextensio_connect.yaml"
	}
	content, err := ioutil.ReadFile(MyYaml + yamltemplate)
	if err != nil {
		log.Fatal(err)
	}
	vservice := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(vservice, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reAgent := regexp.MustCompile(`REPLACE_AGENT_NAME`)
	agentRepl := reAgent.ReplaceAllString(podRepl, agent)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(agentRepl, gateway)

	return gwRepl
}

func GetAppVservice(namespace string, gateway string, podname string, app string, utype string) string {
	var yamltemplate string
	if utype == "A" {
		yamltemplate = "/nextensio_for_user.yaml"
	} else {
		yamltemplate = "/nextensio_for.yaml"
	}
	content, err := ioutil.ReadFile(MyYaml + yamltemplate)
	if err != nil {
		log.Fatal(err)
	}
	vservice := string(content)
	reNspc := regexp.MustCompile(`REPLACE_NAMESPACE`)
	nspcRepl := reNspc.ReplaceAllString(vservice, namespace)
	rePod := regexp.MustCompile(`REPLACE_POD_NAME`)
	podRepl := rePod.ReplaceAllString(nspcRepl, podname)
	reAgent := regexp.MustCompile(`REPLACE_APP_NAME`)
	agentRepl := reAgent.ReplaceAllString(podRepl, app)
	reGw := regexp.MustCompile(`REPLACE_GW`)
	gwRepl := reGw.ReplaceAllString(agentRepl, gateway)

	return gwRepl
}

func GetService(namespace string, podname string) string {
	content, err := ioutil.ReadFile(MyYaml + "/service.yaml")
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

func GetDeploy(namespace string, image string, mongo string, podname string, cluster string, dns string, podtype string) string {
	var yamltemplate string
	if podtype == "C" {
		yamltemplate = "/deploy_cpod.yaml"
	} else {
		yamltemplate = "/deploy_apod.yaml"
	}
	content, err := ioutil.ReadFile(MyYaml + yamltemplate)
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
	reDns := regexp.MustCompile(`REPLACE_MY_DNS`)
	dnsRepl := reDns.ReplaceAllString(cluRepl, dns)

	return dnsRepl
}

func GetConsul(myip string, cluster string) string {
	content, err := ioutil.ReadFile(MyYaml + "/consul.yaml")
	if err != nil {
		log.Fatal(err)
	}
	consul := string(content)
	reCsl := regexp.MustCompile(`REPLACE_SELF_NODE_IP`)
	csRepl := reCsl.ReplaceAllString(consul, myip)
	reClus := regexp.MustCompile(`REPLACE_CLUSTER`)
	clusRepl := reClus.ReplaceAllString(csRepl, cluster)

	return clusRepl
}
