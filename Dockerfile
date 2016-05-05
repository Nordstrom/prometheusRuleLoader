FROM nordstrom/baseimage-ubuntu:16.04
MAINTAINER Innovation Platform Team "invcldtm@nordstrom.com"

COPY PrometheusRuleLoader /bin/PrometheusRuleLoader
RUN chmod 755 /bin/PrometheusRuleLoader


ENTRYPOINT	["/bin/PrometheusRuleLoader"]