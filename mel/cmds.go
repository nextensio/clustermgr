package main

import (
	"flag"
	"fmt"
)

func Cmdline() bool {
	gw := flag.String("gw", "none", "a string")
	namespace := flag.String("nspc", "none", "a string")
	myip := flag.String("myip", "none", "a string")
	image := flag.String("image", "none", "a string")
	mongo := flag.String("mongo", "none", "a string")
	pod := flag.String("pod", "none", "a string")
	agent := flag.String("agent", "none", "a string")
	cluster := flag.String("cluster", "none", "a string")
	dns := flag.String("dns", "none", "a string")

	cmds := flag.Bool("cmds", false, "a bool")
	replIgw := flag.Bool("repl_igw", false, "a bool")
	replEgw := flag.Bool("repl_egw", false, "a bool")
	replEgwDst := flag.Bool("repl_egw_dst", false, "a bool")
	replExt := flag.Bool("repl_ext", false, "a bool")
	replConsul := flag.Bool("repl_csl", false, "a bool")
	replDeploy := flag.Bool("repl_dpl", false, "a bool")
	replAgtVsvc := flag.Bool("repl_agent", false, "a bool")
	replAppVsvc := flag.Bool("repl_app", false, "a bool")
	replSvc := flag.Bool("repl_svc", false, "a bool")

	flag.Parse()

	if *cmds == false {
		return false
	}

	// TODO: revisit this
	if *replIgw == true {
		fmt.Println(GetIngressGw(*gw))
	} else if *replEgw == true {
		fmt.Println(GetEgressGw(*gw))
	} else if *replEgwDst == true {
		fmt.Println(GetEgressGwDst(*gw))
	} else if *replExt == true {
		fmt.Println(GetExtSvc(*gw))
	} else if *replConsul == true {
		fmt.Println(GetConsul(*myip, *cluster))
	} else if *replDeploy == true {
		fmt.Println(GetCpodDeploy(*namespace, *image, *mongo, *pod, *cluster, *dns))
	} else if *replAgtVsvc == true {
		fmt.Println(GetCpodConnectService(*namespace, *gw, *pod, *agent))
	} else if *replAppVsvc == true {
		fmt.Println(GetNxtForCpodService(*namespace, *gw, *pod, *agent))
	} else if *replSvc == true {
		fmt.Println(GetOutsideService(*namespace, *pod))
	}

	return true
}
