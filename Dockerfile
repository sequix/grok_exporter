FROM ubuntu:16.04
LABEL Maintainer="sequix <https://github.com/sequix>"
LABEL Name="grok_exporter"
LABEL Version="1.0.0"

ENV GROK_ARCH="grok_exporter-1.0.0.linux-amd64"
ENV GROK_VERSION="v1.0.0"

RUN apt-get update -qqy \
    && apt-get upgrade -qqy \
    && apt-get install --no-install-recommends -qqy \
       wget unzip ca-certificates \
    && update-ca-certificates \
    && wget https://github.com/sequix/grok_exporter/releases/download/$GROK_VERSION/$GROK_ARCH.zip \
    && unzip $GROK_ARCH.zip \
    && mv $GROK_ARCH /grok \
    && rm $GROK_ARCH.zip \
    && apt-get --autoremove purge -qqy \
       wget unzip ca-certificates \
    && rm -fr /var/lib/apt/lists/*


RUN mkdir -p /etc/grok_exporter
RUN ln -sf /etc/grok_exporter/config.yml /grok/
WORKDIR /grok

CMD ["./grok_exporter", "-config", "/grok/config.yml"]