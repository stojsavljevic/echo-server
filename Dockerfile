FROM scratch
ARG TARGETPLATFORM
COPY artifacts/build/release/$TARGETPLATFORM/echo-server /bin/echo-server
ENV PORT=8080
ENV GRPC_PORT=9090
EXPOSE 8080 9090
ENTRYPOINT ["/bin/echo-server"]
