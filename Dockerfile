FROM debian
ARG DEBIAN_FRONTEND=noninteractive

RUN \
  apt-get update && apt-get -my install curl gnupg && \
  curl -s 'https://download.ceph.com/keys/release.asc' | apt-key add - && \
  echo deb http://download.ceph.com/debian-kraken/ jessie main > /etc/apt/sources.list.d/ceph.list && \
  apt-get update && \
  apt-get -my install librados-dev librbd-dev rsync telnet && \
  rm -rf /var/lib/apt/lists/* && \
  mkdir /etc/cephback

ADD cephback_binary /cephback
RUN chmod 0755 /cephback

ENV HOME /etc/cephback

EXPOSE 9090

ENTRYPOINT /cephback
