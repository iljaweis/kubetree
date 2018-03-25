package main

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
	//"k8s.io/client-go/tools/clientcmd"
	//appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCases(t *testing.T) {

	tests := []struct {
		name   string
		setup  func(kubernetes.Interface)
		expect string
	}{
		{
			name: "empty kubernetes",
			setup: func(k kubernetes.Interface) {

			},
			expect: "kubernetes\n",
		},
		{
			name: "with kube-system",
			setup: func(k kubernetes.Interface) {
				k.CoreV1().Namespaces().Create(
					&corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: "kube-system",
						},
					})
			},
			expect: "kubernetes\n  ns/kube-system\n",
		},
	}

	for _, test := range tests {
		k := New(fake.NewSimpleClientset())

		test.setup(k.kube)

		if kt := k.kubetree(); kt != test.expect {
			t.Errorf("testcase '%s': we expected '%s' but got '%s'", test.name, test.expect, kt)
		}
	}
}
