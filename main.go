package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rancher/types/apis/management.cattle.io/v3"
	"github.com/rancher/types/config"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	CattleControllerName   = "controller.cattle.io"
	DefaultCattleNamespace = "cattle-system"
	CattleLabelBase        = "cattle.io"
)

var VERSION = "v0.0.1-dev"

var staticClusterRoles = []string{
	"cluster-owner",
	"create-ns",
	"project-owner",
	"project-owner-promoted",
}
var cattleNamespace = DefaultCattleNamespace
var cattleListOptions = v1.ListOptions{
	LabelSelector: "cattle.io/creator=norman",
}
var deletePolicy = v1.DeletePropagationBackground

func main() {
	app := cli.NewApp()
	app.Name = "rmrancher"
	app.Version = VERSION
	app.Usage = "A tool to uninstall rancher 2.0 deployments"
	app.Action = doRemoveRancher
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "kubeconfig,c",
			EnvVar: "KUBECONFIG",
			Usage:  "kubeconfig absolute path",
		},
		cli.StringFlag{
			Name:  "namespace,n",
			Usage: "rancher 2.0 deployment namespace. default is `cattle-system`",
		},
	}

	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

func doRemoveRancher(ctx *cli.Context) error {
	// setup
	if ctx.String("namespace") != "" {
		cattleNamespace = ctx.String("namespace")
	}
	restConfig, err := getRestConfig(ctx)
	if err != nil {
		return err
	}
	management, err := config.NewManagementContext(*restConfig)
	if err != nil {
		return err
	}
	k8sClient, err := getClientSet(ctx)
	if err != nil {
		return err
	}
	// getting high-level crd lists
	projects, err := getProjectList(management)
	if err != nil {
		return err
	}
	clusters, err := getClusterList(management)
	if err != nil {
		return err
	}
	users, err := getUserList(management)
	if err != nil {
		return err
	}
	// starting cleanup
	if err := namespacesCleanup(k8sClient); err != nil {
		return err
	}

	if err := secretsCleanup(k8sClient); err != nil {
		return err
	}

	for _, project := range projects {
		logrus.Infof("deleting project [%s]..", project.Name)
		if err := deleteNamespace(k8sClient, project.Name); err != nil && !errors.IsNotFound(err) {
			return err
		}
		if err := deleteProject(management, project); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	for _, cluster := range clusters {
		logrus.Infof("deleting cluster [%s]..", cluster.Name)
		if err := deleteNamespace(k8sClient, cluster.Name); err != nil && !errors.IsNotFound(err) {
			return err
		}
		if err := deleteCluster(management, cluster); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	for _, user := range users {
		logrus.Infof("deleting user [%s]..", user.Name)
		if err := deleteNamespace(k8sClient, user.Name); err != nil && !errors.IsNotFound(err) {
			return err
		}
		if err := deleteUser(management, user); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	clusterRoles, err := getCattleClusterRolesList(k8sClient)
	if err != nil {
		return err
	}
	clusterRoles = append(clusterRoles, staticClusterRoles...)
	for _, clusterRole := range clusterRoles {
		logrus.Infof("deleting cluster role [%s]..", clusterRole)
		if err := deleteClusterRole(k8sClient, clusterRole); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	clusterRoleBindings, err := getCattleClusterRoleBindingsList(k8sClient)
	if err != nil {
		return err
	}

	for _, clusterRoleBinding := range clusterRoleBindings {
		logrus.Infof("deleting cluster role binding [%s]..", clusterRoleBinding)
		if err := deleteClusterRoleBinding(k8sClient, clusterRoleBinding); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	// final cleanup
	logrus.Infof("removing rancher deployment namespace [%s]", cattleNamespace)
	return deleteNamespace(k8sClient, cattleNamespace)
}

func getClientSet(ctx *cli.Context) (*kubernetes.Clientset, error) {
	config, _ := getRestConfig(ctx)
	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func getRestConfig(ctx *cli.Context) (*rest.Config, error) {
	kubeconfig := ctx.String("kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func getProjectList(mgmtCtx *config.ManagementContext) ([]v3.Project, error) {
	projectList, err := mgmtCtx.Management.Projects("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return projectList.Items, nil
}

func getUserList(mgmtCtx *config.ManagementContext) ([]v3.User, error) {
	userList, err := mgmtCtx.Management.Users("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return userList.Items, nil
}

func getClusterList(mgmtCtx *config.ManagementContext) ([]v3.Cluster, error) {
	clusterList, err := mgmtCtx.Management.Clusters("").List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return clusterList.Items, nil
}

func deleteProject(mgmtCtx *config.ManagementContext, project v3.Project) error {

	return mgmtCtx.Management.Projects(project.Namespace).Delete(project.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteCluster(mgmtCtx *config.ManagementContext, cluster v3.Cluster) error {

	return mgmtCtx.Management.Clusters("").Delete(cluster.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteUser(mgmtCtx *config.ManagementContext, user v3.User) error {

	return mgmtCtx.Management.Users("").Delete(user.Name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteNamespace(client *kubernetes.Clientset, name string) error {

	return client.CoreV1().Namespaces().Delete(name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteClusterRole(client *kubernetes.Clientset, name string) error {
	return client.RbacV1().ClusterRoles().Delete(name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func deleteClusterRoleBinding(client *kubernetes.Clientset, name string) error {

	return client.RbacV1().ClusterRoleBindings().Delete(name, &v1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: new(int64),
	})
}

func getCattleClusterRoleBindingsList(client *kubernetes.Clientset) ([]string, error) {
	crbList, err := client.RbacV1().ClusterRoleBindings().List(cattleListOptions)
	if err != nil {
		return nil, err
	}
	crbNames := []string{}
	for _, crb := range crbList.Items {
		crbNames = append(crbNames, crb.Name)
	}

	return crbNames, nil
}

func getCattleClusterRolesList(client *kubernetes.Clientset) ([]string, error) {
	crList, err := client.RbacV1().ClusterRoles().List(cattleListOptions)
	if err != nil {
		return nil, err
	}
	crNames := []string{}
	for _, cr := range crList.Items {
		crNames = append(crNames, cr.Name)
	}
	return crNames, nil
}

func cleanupFinalizers(finalizers []string) []string {
	updatedFinalizers := []string{}
	for _, f := range finalizers {
		if strings.Contains(f, CattleControllerName) {
			continue
		}
		updatedFinalizers = append(updatedFinalizers, f)
	}
	return updatedFinalizers
}

func cleanupAnnotationsLabels(m map[string]string) map[string]string {
	for k := range m {
		if strings.Contains(k, CattleLabelBase) {
			delete(m, k)
		}
	}
	return m
}
func getNamespacesList(client *kubernetes.Clientset) ([]string, error) {

	nsList, err := client.CoreV1().Namespaces().List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	nsNames := []string{}
	for _, ns := range nsList.Items {
		nsNames = append(nsNames, ns.Name)
	}
	return nsNames, nil
}

func secretsCleanup(client *kubernetes.Clientset) error {
	// cleanup finalizers..
	secrets, err := client.CoreV1().Secrets("").List(v1.ListOptions{})
	if err != nil {
		return err
	}
	errs := []error{}
	for _, secret := range secrets.Items {
		if len(secret.Finalizers) == 0 {
			continue
		}
		finalizers := cleanupFinalizers(secret.Finalizers)
		annotations := cleanupAnnotationsLabels(secret.Annotations)
		labels := cleanupAnnotationsLabels(secret.Labels)
		if len(finalizers) != len(secret.Finalizers) ||
			len(annotations) != len(secret.Annotations) ||
			len(labels) != len(secret.Labels) {
			secret.Finalizers = finalizers
			secret.Annotations = annotations
			secret.Labels = labels
			_, err := client.CoreV1().Secrets(secret.Namespace).Update(&secret)
			if err != nil {
				logrus.Infof("%v", err)
				errs = append(errs, err)
			}
			logrus.Infof("cleaned secret %s/%s", secret.Namespace, secret.Name)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

func namespacesCleanup(client *kubernetes.Clientset) error {
	nsList, err := client.CoreV1().Namespaces().List(v1.ListOptions{})
	if err != nil {
		return err
	}
	errs := []error{}
	for _, ns := range nsList.Items {
		finalizers := cleanupFinalizers(ns.Finalizers)
		annotations := cleanupAnnotationsLabels(ns.Annotations)
		labels := cleanupAnnotationsLabels(ns.Labels)
		if len(finalizers) != len(ns.Finalizers) ||
			len(annotations) != len(ns.Annotations) ||
			len(labels) != len(ns.Labels) {
			ns.Finalizers = finalizers
			ns.Annotations = annotations
			ns.Labels = labels
			if _, err = client.CoreV1().Namespaces().Update(&ns); err != nil {
				errs = append(errs, err)
			}
			logrus.Infof("cleaned namespace %s", ns.Name)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}
