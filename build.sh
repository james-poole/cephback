docker build -t cephbackbuild -f Dockerfile_build  .
docker run cephbackbuild cat /go/bin/cephback > cephback_binary
docker build -t jameseckersall/cephback:latest .
docker push jameseckersall/cephback:latest
