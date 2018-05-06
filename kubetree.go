package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fatih/color"
)

type State int

const (
	StateOK       State = 0
	StateWarning  State = 1
	StateCritical State = 2
	StateNone     State = 3
)

type Element struct {
	name      string
	namespace string
	kind      string
	title     string
	object    interface{}
	parent    *Element
	children  map[string]*Element
	state     State
	stateDesc string
}

type Kubetree struct {
	kube                        kubernetes.Interface
	root                        *Element
	namespace                   string
	lookup                      map[string]map[string]*Element
	useColor                    bool
	Green, Yellow, Red, NoColor *color.Color
}

func main() {

	var kubeconfig, namespace string
	var useColor bool

	flag.StringVar(&kubeconfig, "kubeconfig", "", "location of kubeconfig")
	flag.StringVar(&namespace, "n", "", "namespace; all of empty for all namespaces")
	flag.BoolVar(&useColor, "c", false, "color; use color for resource state")

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
	k.useColor = useColor

	if useColor {
		k.Green = color.New(color.FgGreen)
		k.Yellow = color.New(color.FgYellow)
		k.Red = color.New(color.FgRed)
	} else {
		k.Green = color.New()
		k.Yellow = color.New()
		k.Red = color.New()
	}
	k.NoColor = color.New()

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
	k.root.state = StateNone

	if err := k.addNamespaces(); err != nil {
		panic(err)
	}

	if err := k.addDeployments(); err != nil {
		panic(err)
	}

	if err := k.addReplicaSets(); err != nil {
		panic(err)
	}

	if err := k.addStatefulSets(); err != nil {
		panic(err)
	}

	if err := k.addDaemonSets(); err != nil {
		panic(err)
	}

	if err := k.addPersistentVolumeClaims(); err != nil {
		panic(err)
	}

	if err := k.addPersistentVolumes(); err != nil {
		panic(err)
	}

	if err := k.addServices(); err != nil {
		panic(err)
	}

	if err := k.addPods(); err != nil {
		panic(err)
	}

	k.printTree(&b, k.root, "")

	return b.String()
}

func (k *Kubetree) addServices() error {
	services, err := k.kube.CoreV1().Services(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for _, o := range services.Items {
		o := o
		e := newElement(o.Name, &o, "Service", "svc/"+o.Name, nil, o.Namespace)

		switch o.Spec.Type {
		case corev1.ServiceTypeLoadBalancer:
			ok := false
			for _, i := range o.Status.LoadBalancer.Ingress {
				if len(i.IP) > 0 {
					e.stateDesc = i.IP
					ok = true
				}
				if len(i.Hostname) > 0 {
					e.stateDesc = i.Hostname
					ok = true
				}
			}
			if !ok {
				e.stateDesc = "pending"
				e.state = StateWarning
			}
		}

		k.lookup["Service"][o.Namespace+"/"+o.Name] = e
		pods := k.findPodsWithLabels(o.Namespace, o.Spec.Selector)
		for _, pod := range pods {
			e.parent = k.controllerOf(pod)
			e.parent.children["svc/"+o.Name] = e
		}
	}

	return nil
}

func (k *Kubetree) addPersistentVolumes() error {
	pvs, err := k.kube.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, o := range pvs.Items {
		o := o
		// Volumes will only appear if bound to a PVC.
		e := newElement(o.Name, &o, "PersistentVolume", "pv/"+o.Name, nil, "")

		switch o.Status.Phase {
		case corev1.VolumePending:
			e.state = StateWarning
			e.stateDesc = "pending"
		case corev1.VolumeReleased:
			e.state = StateWarning
			e.stateDesc = "released" // should not be attached to anything
		case corev1.VolumeFailed:
			e.state = StateCritical
			e.stateDesc = "failed"
		}

		k.lookup["PersistentVolume"][o.Name] = e
		if o.Status.Phase == corev1.VolumeBound && o.Spec.ClaimRef.Kind == "PersistentVolumeClaim" {
			claimName := o.Spec.ClaimRef.Namespace + "/" + o.Spec.ClaimRef.Name
			if pvc, ok := k.lookup["PersistentVolumeClaim"][claimName]; ok {
				e.parent = pvc
				pvc.children["pv/"+o.Name] = e
			}
		}
	}

	return nil
}

func (k *Kubetree) addPersistentVolumeClaims() error {
	pvcs, err := k.kube.CoreV1().PersistentVolumeClaims(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, o := range pvcs.Items {
		o := o
		// Claims will be added by their pods later
		e := newElement(o.Name, &o, "PersistentVolumeClaim", "pvc/"+o.Name, nil, o.Namespace)

		switch o.Status.Phase {
		case corev1.ClaimPending:
			e.state = StateWarning
			e.stateDesc = "pending"
		case corev1.ClaimBound:
		case corev1.ClaimLost:
			e.state = StateCritical
			e.stateDesc = "lost"
		}

		k.lookup["PersistentVolumeClaim"][o.Namespace+"/"+o.Name] = e
	}

	return nil
}

func (k *Kubetree) addDaemonSets() error {
	daemonsets, err := k.kube.ExtensionsV1beta1().DaemonSets(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for _, o := range daemonsets.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "DaemonSet", "ds/"+o.Name, parents[0], o.Namespace)

		current := o.Status.CurrentNumberScheduled
		ready := o.Status.NumberReady
		missched := o.Status.NumberMisscheduled
		max := o.Status.DesiredNumberScheduled

		var misStatus string
		if missched == 0 {
			misStatus = ""
		} else {
			misStatus = fmt.Sprintf(" (%d misscheduled)", missched)
		}

		if missched > 0 || current < max || ready < max {
			e.state = StateCritical
		}

		e.stateDesc = fmt.Sprintf("%d/%d up, %d/%d pods up%s", current, max, ready, max, misStatus)

		k.lookup["DaemonSet"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["ds/"+o.Name] = e
		}
	}

	return nil
}

func (k *Kubetree) addStatefulSets() error {
	statefulsets, err := k.kube.AppsV1beta2().StatefulSets(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for _, o := range statefulsets.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "StatefulSet", "statefulsets/"+o.Name, parents[0], o.Namespace)

		current := o.Status.CurrentReplicas
		max := o.Status.Replicas
		ready := o.Status.ReadyReplicas

		e.stateDesc = fmt.Sprintf("%d/%d repl, %d/%d rdy", current, max, ready, max)

		if current < max || ready < max {
			e.state = StateCritical
		}

		k.lookup["StatefulSet"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["statefulsets/"+o.Name] = e
		}
	}

	return nil
}

func (k *Kubetree) addReplicaSets() error {
	replicasets, err := k.kube.ExtensionsV1beta1().ReplicaSets(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for _, o := range replicasets.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "ReplicaSet", "rs/"+o.Name, parents[0], o.Namespace)

		avail := o.Status.AvailableReplicas
		ready := o.Status.ReadyReplicas
		max := o.Status.Replicas

		e.stateDesc = fmt.Sprintf("%d/%d up, %d/%d rdy", avail, max, ready, max)

		if avail < max || ready < max {
			if avail != 0 && ready != 0 {
				e.state = StateWarning
			}
			e.state = StateCritical
		}

		k.lookup["ReplicaSet"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["rs/"+o.Name] = e
		}
	}

	return nil
}

func (k *Kubetree) addDeployments() error {
	deployments, err := k.kube.ExtensionsV1beta1().Deployments(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, o := range deployments.Items {
		o := o
		parents := k.parents(o.ObjectMeta)
		e := newElement(o.Name, &o, "Deployment", "deploy/"+o.Name, parents[0], o.Namespace)

		avail := o.Status.AvailableReplicas
		updated := o.Status.UpdatedReplicas
		max := o.Status.Replicas

		e.stateDesc = fmt.Sprintf("%d/%d av, %d/%d up to date", avail, max, updated, max)

		if avail < max || updated < max {
			if avail != 0 && updated != 0 {
				e.state = StateWarning
			}
			e.state = StateCritical
		}

		k.lookup["Deployment"][o.Namespace+"/"+o.Name] = e
		for _, p := range parents {
			p.children["deploy/"+o.Name] = e
		}
	}
	return nil
}

func (k *Kubetree) addNamespaces() error {
	var ns []corev1.Namespace
	if k.namespace != metav1.NamespaceAll {
		n, err := k.kube.CoreV1().Namespaces().Get(k.namespace, metav1.GetOptions{})
		if err != nil {
			return err
		}
		ns = []corev1.Namespace{*n}

	} else {
		nn, err := k.kube.CoreV1().Namespaces().List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		ns = nn.Items
	}
	for _, n := range ns {
		e := newElement(n.Name, &n, "Namespace", "ns/"+n.Name, k.root, "")
		e.state = StateNone
		k.root.children[n.Name] = e
		k.lookup["Namespace"][n.Name] = e
	}

	return nil
}

func (k *Kubetree) addPods() error {
	pods, err := k.kube.CoreV1().Pods(k.namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, o := range pods.Items {
		pod := o
		parents := k.parents(pod.ObjectMeta)
		e := newElement(pod.Name, &pod, "Pod", "po/"+pod.Name, parents[0], pod.Namespace)

		ready, running, max := 0, 0, len(pod.Status.ContainerStatuses)

		for _, c := range pod.Status.ContainerStatuses {
			if c.Ready {
				ready++
			}
			if c.State.Running != nil {
				running++
			}
		}

		e.stateDesc = fmt.Sprintf("%d/%d up, %d/%d rdy", running, max, ready, max)

		switch pod.Status.Phase {
		case corev1.PodRunning:
			if running != max || ready != max {
				e.state = StateCritical
			}
		case corev1.PodPending:
			e.state = StateWarning
			e.stateDesc = e.stateDesc + " (pending)"
		case corev1.PodSucceeded:
			e.stateDesc = e.stateDesc + " (succeeded)"
		case corev1.PodFailed:
			e.state = StateCritical
			e.stateDesc = e.stateDesc + " (failed)"
		}

		for _, v := range pod.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				claimName := pod.Namespace + "/" + v.PersistentVolumeClaim.ClaimName
				if pvc, ok := k.lookup["PersistentVolumeClaim"][claimName]; ok {
					pvc.parent = e
					e.children[claimName] = pvc
				}
			}
		}

		k.lookup["Pod"][pod.Namespace+"/"+pod.Name] = e
		for _, p := range parents {
			p.children["po/"+pod.Name] = e
		}
	}

	return nil
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
	var col *color.Color

	switch e.state {
	case StateOK:
		col = k.Green
	case StateWarning:
		col = k.Yellow
	case StateCritical:
		col = k.Red
	default:
		col = k.NoColor
	}

	b.WriteString(fmt.Sprintf("%s", indent))
	b.WriteString(col.Sprintf("%s", e.title))
	if len(e.stateDesc) > 0 {
		b.WriteString(k.NoColor.Sprintf(" -- %s", e.stateDesc))
	}
	b.WriteString("\n")

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
