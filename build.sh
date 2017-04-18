docker build -t cephbackbuild -f Dockerfile_build  .
if [ "$?" != "0" ]; then
  echo "Build of build image failed"
  exit 1
fi
docker run cephbackbuild cat /go/bin/cephback > cephback_binary
if [ "$?" != "0" ]; then
  echo "Extract of binary from build image failed"
  exit 1
fi
docker build -t jameseckersall/cephback:latest .
if [ "$?" != "0" ]; then
  echo "Build of final image failed"
  exit 1
fi
docker push jameseckersall/cephback:latest
if [ "$?" != "0" ]; then
  echo "Push failed"
  exit 1
fi
