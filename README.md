This tool watches a volume mounted configmap directory and kubernetes events for services with annotations labeled "nordstrom.net/alerts/prometheus" for Prometheus alerting rules. If they are found they are validated and placed into two files (one for annotations and one for configmaps) then the passed enpoint is hit.

Parameters
==========

*  -cmrules - Takes a filename where you would like the configmap rules to be written
*  -svrules - Takes a filename where you would like the service annotation rules to be written
*  -map - Path where the configmap is mounted
*  -endpoint - Endpoint to make a bodyless POST request to (Prometheus uses /-/reload)

All of these parameters will by default use the following envriomental variables:

* cmrules defaults to 'CM_RULES_LOCATION'
* svrules defaults to 'SV_RULES_LOCATION'
* map defaults to 'CONFIG_MAP_LOCATION'
* endpoint defaults to 'PROMETHEUS_RELOAD_ENDPOINT'

for example:
./PrometheusRuleLoader -cmrules ./cmrules.rules -svrules ./svrules.rules -map ./config -endpoint http://prometheus.cluster.local/-/reload

Service Annotations
===================
The annotation supports either a json string or a json array. The annotation must have the key "nordstrom.net/alerts/prometheus"

Examples:



	---
	apiVersion: v1
	kind: Service
	metadata:
	  name: fakeservice
	  namespace: default
	  labels:
	    app: fake
	  annotations: {
	    "nordstrom.net/alerts/prometheus" : "\"ALERT DiskFilling
	    IF (container_fs_usage_bytes{id='/'} / container_fs_limit_bytes{id='/'})*100 > 80.0
	    FOR 5m
	    LABELS { severity = 'critical' }
	    ANNOTATIONS {
	      summary = 'Disk {{ $labels.device }} on host {{ $labels.instance }} filling up.',
	      description = 'Disk {{ $labels.device }} on host {{ $labels.instance }} ({{ $labels.aws_amazon_com_instance_id }}) is filling up. Disk is {{ $value }} full at the time of alerting.'
	      }\""
	  }
	spec:
	  ports:
	    - name: foo
	      port: 80
	  selector:
	    app: foo

OR

	---
	apiVersion: v1
	kind: Service
	metadata:
	  name: fakeservice
	  namespace: default
	  labels:
	    app: fake
	  annotations: {
	    "nordstrom.net/alerts/prometheus" : "[\"ALERT DiskFilling
	    IF (container_fs_usage_bytes{id='/'} / container_fs_limit_bytes{id='/'})*100 > 80.0
	    FOR 5m
	    LABELS { severity = 'critical' }
	    ANNOTATIONS {
	      summary = 'Disk {{ $labels.device }} on host {{ $labels.instance }} filling up.',
	      description = 'Disk {{ $labels.device }} on host {{ $labels.instance }} ({{ $labels.aws_amazon_com_instance_id }}) is filling up. Disk is {{ $value }} full at the time of alerting.'
	      }\",
	      \"Another rule!\"
	      ]"
	  }
	spec:
	  ports:
	    - name: foo
	      port: 80
	  selector:
	    app: foo

Other Notes
===========
Every rule is verified and only ones that pass are added to the configuration files. If you see a rule failing to pass you can use the promtool that ships with prometheus to verify it.
