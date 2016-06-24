FROM quay.io/nordstrom/baseimage-ubuntu:16.04
MAINTAINER Innovation Platform Team "invcldtm@nordstrom.com"

COPY prometheusRuleLoader /bin/prometheusRuleLoader
ENTRYPOINT	["/bin/prometheusRuleLoader"]