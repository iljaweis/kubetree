# kubetree

kubetree lists all resources in a cluster as a tree.

```bash
$ kubetree
kubernetes
  ns/default
    rs/replicent
      po/replicent-5qljt
      po/replicent-9kjpm
      po/replicent-p6x2p
    statefulsets/centostate
      svc/centostate
      po/centostate-0
        pvc/myvol-centostate-0
          pv/pvc-9ba747ad-303b-11e8-8f15-08002703c531
      po/centostate-1
        pvc/myvol-centostate-1
          pv/pvc-c02c0beb-303b-11e8-8f15-08002703c531
      po/centostate-2
        pvc/myvol-centostate-2
          pv/pvc-c0c647c4-303b-11e8-8f15-08002703c531
  ns/kube-public
  ns/kube-system
    deploy/kube-dns
      svc/kube-dns
      rs/kube-dns-54cccfbdf8
        po/kube-dns-54cccfbdf8-mkbqz
    deploy/kubernetes-dashboard
      svc/kubernetes-dashboard
      rs/kubernetes-dashboard-77d8b98585
        po/kubernetes-dashboard-77d8b98585-wfdsf
    po/kube-addon-manager-minikube
    po/storage-provisioner
```

## Options

- **-n NAMESPACE** list only resource of NAMESPACE. "all", the default lists all.
- **-kubeconfig** points to the kubeconfig. Default `~/.kube/config` or the value of environment `KUBECONFIG`.
