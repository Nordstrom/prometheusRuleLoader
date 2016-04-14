FROM nordstrom/baseimage-ubuntu:16.04
MAINTAINER Innovation Platform Team "invcldtm@nordstrom.com"

COPY PrometheusRuleLoader /PrometheusRuleLoader

ENTRYPOINT	[/PrometheusRuleLoader]