package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	iflag "github.com/skupperproject/skupper/internal/flag"
	"github.com/skupperproject/skupper/internal/kube/adaptor"
	internalclient "github.com/skupperproject/skupper/internal/kube/client"
	"github.com/skupperproject/skupper/internal/utils"
	"github.com/skupperproject/skupper/internal/version"
)

var onlyOneSignalHandler = make(chan struct{})
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

func SetupSignalHandler() (stopCh <-chan struct{}) {
	close(onlyOneSignalHandler) // panics when called twice

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return stop
}

func main() {
	flags := flag.NewFlagSet("", flag.ExitOnError)

	var namespace string
	var kubeconfig string
	iflag.StringVar(flags, &namespace, "namespace", "NAMESPACE", "", "The Kubernetes namespace scope for the controller")
	iflag.StringVar(flags, &kubeconfig, "kubeconfig", "KUBECONFIG", "", "A path to the kubeconfig file to use")

	var configDir string
	var configMapName string
	iflag.StringVar(flags, &configDir, "config-dir", "SKUPPER_CONFIG_DIR", "/etc/skupper-router-certs", "The directory to which configuration should be saved")
	iflag.StringVar(flags, &configMapName, "router-config", "SKUPPER_ROUTER_CONFIG", "skupper-router", "The name of the ConfigMap containing the router config")

	// if -version used, report and exit
	isVersion := flags.Bool("version", false, "Report the version of Config Sync")
	isInit := flags.Bool("init", false, "Downloads configuration and ssl profile artefacts")
	flags.Parse(os.Args[1:])
	if *isVersion {
		fmt.Println(version.Version)
		os.Exit(0)
	}

	// Startup message
	log.Printf("Version: %s", version.Version)

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := SetupSignalHandler()

	cli, err := internalclient.NewClient(namespace, "", kubeconfig)
	if err != nil {
		log.Fatal("Error getting van client: ", err.Error())
	}

	if *isInit {
		if err := adaptor.InitialiseConfig(cli, cli.GetNamespace(), configDir, configMapName); err != nil {
			log.Fatal("Error initialising config ", err.Error())
		}
		os.Exit(0)
	}

	log.Println("Waiting for Skupper router to be ready")
	_, err = waitForPodsSelectorStatus(cli.GetNamespace(), cli.Kube, "skupper.io/component=router", corev1.PodRunning, time.Second*180, time.Second*5)
	if err != nil {
		log.Fatal("Error waiting for router pods to be ready ", err.Error())
	}

	log.Println("Starting collector...")
	go adaptor.StartCollector(cli)

	//start health check
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":9191", nil)

	configSync := adaptor.NewConfigSync(cli, cli.GetNamespace(), configDir, configMapName)
	log.Println("Starting controller loop...")
	configSync.Start(stopCh)

	<-stopCh
	log.Println("Shutting down...")
	configSync.Stop()
}

func getPods(selector string, namespace string, cli kubernetes.Interface) ([]corev1.Pod, error) {
	options := metav1.ListOptions{LabelSelector: selector}
	podList, err := cli.CoreV1().Pods(namespace).List(context.TODO(), options)
	if err != nil {
		return nil, err
	}
	return podList.Items, err
}

func waitForPodsSelectorStatus(namespace string, clientset kubernetes.Interface, selector string, status corev1.PodPhase, timeout time.Duration, interval time.Duration) ([]corev1.Pod, error) {
	var pods []corev1.Pod
	var pod corev1.Pod
	var err error

	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	err = utils.RetryWithContext(ctx, interval, func() (bool, error) {
		pods, err = getPods(selector, namespace, clientset)
		if err != nil {
			// pod does not exist yet
			return false, nil
		}
		for _, pod = range pods {
			if pod.Status.Phase != status {
				return false, nil
			}
		}
		return true, nil
	})

	return pods, err
}
