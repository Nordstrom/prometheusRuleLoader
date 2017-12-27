### New version contains breaking changes, see *Changes* below

Handy Kubernetes sidecar for Prometheus 2.x. It watches for configmaps in all namespaces that have the annotation specified with the -annotation flag. When it finds one it pulls each value out and assumes it's a prometheus rule. It then validates them and adds them to a the rules file specified with -rulespath. If that file changes it hits the reload endpoint specified by the -endpoint flag. 

Changes
=======
* New 3.2 version. Added prometheus counters for, successful reloads, failed reloads and number of rule validation failures.

* New 3.1 version. Added back in rule validation, currently works on a group level (i.e. each key in a configmap is a group). If one rule in a group fails the group fails.

* New 3.0 version, breaking changes! This works with the new prometheus 2 rules only, if you want to use the rule loader with prometheus 1.x please use the `prom1.x-stable` tag.


Parameters
==========

*  `-annotation` - Used to customize the annotation label you'd like the rule loader to look like on your configmaps.
*  `-rulespath` - The location you would like your rules to be written to. Should correspond to a rule_files path in your prometheus config.
*  `-endpoint` - Endpoint to make a bodyless POST request to (Prometheus uses /-/reload)
*  `-batchtime` - Configure how long you want it to sleep between reload attempts in seconds, if your configmaps churn a lot it can cause excessive reloads on prometheus.
*  `-kubeconfig` - Use a kubeconfig to configure the connection to the api server, off cluster use only.
*  `master` - Address of the master server, overrides server in kubeconfig. For off cluster use only.


for example:

`./PrometheusRuleLoader -rulespath ./rules.rules -annotation 'prometheus.io/v2/rules' -endpoint http://127.0.0.1:9090/-/reload`

Configmap Annotation
====================
Assuming a launch with the above commandline here is an example of a configmap.

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: 'test-rules'
  namespace: "kube-system"
  labels:
    app: "prometheus"
  annotations: {
    "prometheus.io/v2/rules": "true"
  }
data:
  mygroupname: |
    - record: job:http_inprogress_requests:sum
      expr: sum(http_inprogress_requests) by (job)
    - alert: HighErrorRate
      expr: job:request_latency_seconds:mean5m{job="myjob"} > 0.5
      for: 10m
      labels:
        severity: page
      annotations:
        summary: High request latency
```

Deployment
==========
The PrometheusRuleLoaders docker container should be deployed in the same pod as prometheus. They should both share a volume mount (and emptydir works fine here). PrometheusRuleLoader will use this shared space to write it's rule file to, meanwhile Prometheus should be configured to look for it's rule file at this path.
