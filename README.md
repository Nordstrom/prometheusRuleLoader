Handy Kubernetes sidecar for prometheus. It watches for configmaps in all namespaces that have the annotation specified with the -annotation flag. When it finds one it pulls each value out and assumes it's a prometheus rule. It then validates them and adds them to a the rules file specified with -rulespath. If that file changes it hits the reload endpoint specified by the -endpoint flag. 

Parameters
==========

*  -annotation - Used to customize the annotation label you'd like the rule loader to look like on your configmaps.
*  -rulespath - The location you would like your rules to be written to. Should correspond to a rule_files path in your prometheus config.
*  -endpoint - Endpoint to make a bodyless POST request to (Prometheus uses /-/reload)

All of these parameters will by default use the following envriomental variables:

* annotation defaults to 'CONFIG_MAP_ANNOTATION'
* rulespath defaults to 'RULES_LOCATION'
* endpoint defaults to 'PROMETHEUS_RELOAD_ENDPOINT'

for example:
./PrometheusRuleLoader -rulespath ./rules.rules -annotation 'prometheus.io/rules' -endpoint http://prometheus.kube-system.cluster.local/-/reload

Configmap Annotation
====================
Assuming a lauch with the above commandline here is an example of a configmap.

```

kind: ConfigMap
apiVersion: v1
metadata:
  name: 'test-rules'
  namespace: "kube-system"
  labels:
    app: "prometheus"
  annotations: {
    "prometheus.io/rules": "true"
  }
data:
  toomanyfoos: |
    ALERT TooManyFoosToFast
    IF sum(rate(fooCounter[1m])) > 1
    FOR 1m
    LABELS { severity = "warning" }
    ANNOTATIONS {
      description = "Rate of foo very high ({{ $value }})."
    }
  waytoomanyfoos: |
    ALERT WayToManyFoosToFast
    IF sum(rate(fooCounter[1m])) > 5
    FOR 1m
    LABELS { severity = "critical" }
    ANNOTATIONS {
      description = "Rate of foo critically high ({{ $value }})."
    }


```

Other Notes
===========
Every rule is verified and only ones that pass are added to the configuration files. If you see a rule failing to pass you can use the promtool that ships with prometheus to verify it.
