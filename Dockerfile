FROM debian
ARG DEBIAN_FRONTEND=noninteractive

RUN \
  apt-get update && apt-get -y install curl && \
  curl -s 'https://download.ceph.com/keys/release.asc' | apt-key add - && \
  echo deb http://download.ceph.com/debian-jewel/ jessie main > /etc/apt/sources.list.d/ceph.list && \
  apt-get update && \
  apt-get -y install librados-dev librbd-dev rsync curl telnet && \
  rm -rf /var/lib/apt/lists/*
ADD cephback_binary /cephback
RUN chmod 0755 /cephback
ENTRYPOINT /cephback

EXPOSE 9090
