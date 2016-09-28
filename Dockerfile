FROM quay.io/nordstrom/baseimage-ubuntu:16.04
MAINTAINER Innovation Platform Team "invcldtm@nordstrom.com"

USER root
COPY prometheusRuleLoader /bin/prometheusRuleLoader
RUN chmod 755 /bin/prometheusRuleLoader
USER ubuntu

ENTRYPOINT	["/bin/prometheusRuleLoader"]