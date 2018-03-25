package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	//"github.com/davecgh/go-spew/spew"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	//appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sort"
)

type Element struct {
	name      string
	namespace string
	kind      string
	title     string
	object    interface{}
	parent    *Element
	children  map[string]*Element
}

type Kubetree struct {
	kube      kubernetes.Interface
	root      *Element
	namespace string
	lookup    map[string]map[string]*Element
}

func main() {

	var kubeconfig, namespace string

	flag.StringVar(&kubeconfig, "kubeconfig", "", "location of kubeconfig")
	flag.StringVar(&namespace, "n", "", "namespace; all of empty for all namespaces")

	flag.Parse()

	if namespace == "all" || namespace == "" {
		namespace = metav1.NamespaceAll
	}

	if c := os.Getenv("KUBECONFIG"); c != "" {
		kubeconfig = c
	}

	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	c, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		panic(err.Error())
	}

	k := New(c)
	k.namespace = namespace

	fmt.Print(k.kubetree())
}

func New(kube kubernetes.Interface) *Kubetree {
	return &Kubetree{
		kube: kube,
		lookup: map[string]map[string]*Element{
			"Namespace":             make(map[string]*Element),
			"Deployment":            make(map[string]*Element),
			"ReplicaSet":            make(map[string]*Element),
			"StatefulSet":           make(map[string]*Element),
			"DaemonSet":             make(map[string]*Element),
			"Pod":                   make(map[string]*Element),
			"Service":               make(map[string]*Element),
			"PersistentVolume":      make(map[string]*Element),
			"PersistentVolumeClaim": make(map[string]*Element),
		},
	}
}

func (k *Kubetree) kubetree() string {

	var b strings.Builder

	k.root = newElement("kubernetes", nil, "cluster", "kubernetes", nil, "")

	var ns []corev1.Namespace

	if k.namespace != metav1.NamespaceAll {
		n, err := k.kube.CoreV1().Namespaces().Get(k.namespace, metav1.GetOptions{})
		if err != nil {
			panic(err.Error())
		}
		ns = []corev1.Namespace{*n}

	} else {
		nn, err := k.kube.CoreV1().Namespaces().List(metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		ns = nn.Items
	}

	for _, n := range ns {
		e := newElement(n.Name, &n, "Namespace", "ns/"+n.Name, k.root, "")
		k.root.children[n.Name] = e
		k.lookup["Namespace"][n.Name] = e
	}

	deployments, err := k.kube.ExtensionsV1beta1().Deployments(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, o := range deployments.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "Deployment", "deploy/"+o.Name, parents[0], o.Namespace)
		k.lookup["Deployment"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["deploy/"+o.Name] = e
		}
	}

	replicasets, err := k.kube.ExtensionsV1beta1().ReplicaSets(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, o := range replicasets.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "ReplicaSet", "rs/"+o.Name, parents[0], o.Namespace)
		k.lookup["ReplicaSet"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["rs/"+o.Name] = e
		}
	}

	statefulsets, err := k.kube.AppsV1beta2().StatefulSets(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, o := range statefulsets.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "StatefulSet", "statefulsets/"+o.Name, parents[0], o.Namespace)
		k.lookup["StatefulSet"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["statefulsets/"+o.Name] = e
		}
	}

	daemonsets, err := k.kube.ExtensionsV1beta1().DaemonSets(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, o := range daemonsets.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "DaemonSet", "ds/"+o.Name, parents[0], o.Namespace)
		k.lookup["DaemonSet"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["ds/"+o.Name] = e
		}
	}

	pvcs, err := k.kube.CoreV1().PersistentVolumeClaims(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, o := range pvcs.Items {
		o := o
		// Claims will be added by their pods later
		e := newElement(o.Name, &o, "PersistentVolumeClaim", "pvc/"+o.Name, nil, o.Namespace)
		k.lookup["PersistentVolumeClaim"][o.Namespace+"/"+o.Name] = e
	}

	pvs, err := k.kube.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, o := range pvs.Items {
		o := o
		// Volumes will only appear if bound to a PVC.
		e := newElement(o.Name, &o, "PersistentVolume", "pv/"+o.Name, nil, "")
		k.lookup["PersistentVolume"][o.Name] = e
		if o.Status.Phase == corev1.VolumeBound && o.Spec.ClaimRef.Kind == "PersistentVolumeClaim" {
			claimName := o.Spec.ClaimRef.Namespace + "/" + o.Spec.ClaimRef.Name
			if pvc, ok := k.lookup["PersistentVolumeClaim"][claimName]; ok {
				e.parent = pvc
				pvc.children["pv/"+o.Name] = e
			}
		}
	}

	pods, err := k.kube.CoreV1().Pods(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, o := range pods.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "Pod", "po/"+o.Name, parents[0], o.Namespace)
		for _, v := range o.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				claimName := o.Namespace + "/" + v.PersistentVolumeClaim.ClaimName
				if pvc, ok := k.lookup["PersistentVolumeClaim"][claimName]; ok {
					pvc.parent = e
					e.children[claimName] = pvc
				}
			}
		}

		k.lookup["Pod"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["po/"+o.Name] = e
		}
	}

	services, err := k.kube.CoreV1().Services(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, o := range services.Items {
		o := o
		e := newElement(o.Name, &o, "Service", "svc/"+o.Name, nil, o.Namespace)
		k.lookup["Service"][o.Namespace+"/"+o.Name] = e
		pods := k.findPodsWithLabels(o.Namespace, o.Spec.Selector)
		for _, pod := range pods {
			e.parent = k.controllerOf(pod)
			e.parent.children["svc/"+o.Name] = e
		}
	}

	k.printTree(&b, k.root, "")

	return b.String()
}

func newElement(name string, object interface{}, kind string, title string, parent *Element, namespace string) *Element {
	return &Element{
		name:      name,
		namespace: namespace,
		kind:      kind,
		title:     title,
		object:    object,
		parent:    parent,
		children:  map[string]*Element{},
	}
}

type ElementSort []*Element

func (a ElementSort) Len() int {
	return len(a)
}

func (a ElementSort) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a ElementSort) Less(i, j int) bool {
	if a[i].kind == "Service" {
		return true
	} else if a[j].kind == "Service" {
		return false
	} else {
		return a[i].kind < a[j].kind
	}
}

func (k *Kubetree) printTree(b *strings.Builder, e *Element, indent string) {
	b.WriteString(fmt.Sprintf("%s%s\n", indent, e.title))

	// we want the output sorted
	cc := make([]*Element, 0, len(e.children))
	for _, c := range e.children {
		cc = append(cc, c)
	}
	sort.Sort(ElementSort(cc))
	for _, c := range cc {
		k.printTree(b, c, indent+"  ")
	}
}

func (k *Kubetree) parents(m metav1.ObjectMeta) []*Element {
	if m.Namespace == "" {
		return []*Element{k.root}
	} else if m.OwnerReferences == nil && len(m.OwnerReferences) == 0 {
		return []*Element{k.lookup["Namespace"][m.Namespace]}
	} else {
		var p []*Element
		for _, o := range m.OwnerReferences {
			if _, ok := k.lookup[o.Kind]; ok {
				if pp, ok := k.lookup[o.Kind][m.Namespace+"/"+o.Name]; ok {
					p = append(p, pp)
				}
			}
		}
		if len(p) > 0 {
			return p
		} else {
			return []*Element{k.lookup["Namespace"][m.Namespace]}
		}
	}
}

func (k *Kubetree) findPodsWithLabels(namespace string, labels map[string]string) (es []*Element) {
	for _, e := range k.lookup["Pod"] {
		if pod, ok := e.object.(*corev1.Pod); ok {
			if pod.Labels != nil && len(pod.Labels) > 0 && e.namespace == namespace {
				for l, v := range labels {
					if pod.Labels[l] != v {
						continue
					}
					es = append(es, e)
				}
			}
		} else {
			panic("not a pod here")
		}
	}

	return
}

func (k *Kubetree) controllerOf(e *Element) *Element {
	for e.parent != nil && (e.parent.kind == "ReplicaSet" || e.parent.kind == "StatefulSet" || e.parent.kind == "DaemonSet" || e.parent.kind == "Deployment") {
		e = e.parent
	}
	return e
}
