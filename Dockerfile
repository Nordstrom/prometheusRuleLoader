FROM prom/busybox:latest
MAINTAINER Innovation Platform Team "invcldtm@nordstrom.com"

COPY prometheusRuleLoader /bin/prometheusRuleLoader
RUN chmod 755 /bin/prometheusRuleLoader

ENTRYPOINT	["/bin/prometheusRuleLoader"]
