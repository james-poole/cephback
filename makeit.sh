#!/bin/bash

docker rmi cephback_builder 2>/dev/null
docker build --force-rm -t cephback_builder -f Dockerfile_build .
docker run --rm --name cephback_builder_tmp cephback_builder bash -c "tar Ccf /tmp/deps/ - ." | docker import - cephback
docker rmi cephback_builder 2>/dev/null
