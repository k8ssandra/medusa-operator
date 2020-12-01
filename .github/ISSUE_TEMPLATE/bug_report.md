---
name: Bug report
about: Create a report to help us improve
title: ''
labels: bug, needs-triage
assignees: ''

---

**Describe the bug**
A clear and concise description of what the bug is.

**To Reproduce**
Steps to reproduce the behavior:
1. Go to '...'
2. Click on '....'
3. Scroll down to '....'
4. See error

**Expected behavior**
A clear and concise description of what you expected to happen.

**Screenshots**
If applicable, add screenshots to help explain your problem.

**Environment (please complete the following information):**

* medusa-operator version:
<!-- Insert the image tag or Git SHA here. -->

<!--
    You can try a jsonpath query with kubectl like this to get the version:

        kubectl get deployment <medusa-operator-deployment> \
            -o jsonpath='{.spec.template.spec.containe[0].image}'
-->

<!--
Please provide the follow info if you deployed reaper-operator via the
k8ssandra Helm chart(s). 
-->
* Helm charts version info 
<!-- list installed charts and their versions from all namespaces -->
<!-- Replace the command with its output -->
`$ helm ls -A` 

* Helm charts user-supplied values
<!-- For each k8ssandra chart involved list user-supplied values -->
<!-- Replace the commands with its output -->
`$ helm get values RELEASE_NAME` 

* Kubernetes version information:
<!-- Replace the command with its output -->
`kubectl version`

* Kubernetes cluster kind:
<!-- Insert how you created your cluster: kind, kops, bootkube, etc. -->

* Manifests:

<!-- Please provide any manifests relevant to the issue -->

* Operator logs:

<!-- Please provide any medusa-operator logs relevant to the issue -->

**Additional context**
<!-- Add any other context about the problem here. -->
