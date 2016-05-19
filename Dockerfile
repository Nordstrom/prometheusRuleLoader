FROM nordstrom/baseimage-ubuntu:16.04
MAINTAINER Innovation Platform Team "invcldtm@nordstrom.com"

COPY prometheusRuleLoader /bin/prometheusRuleLoader
RUN chmod 755 /bin/prometheusRuleLoader

ENTRYPOINT	["/bin/prometheusRuleLoader"]