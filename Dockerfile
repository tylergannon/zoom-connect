# Build the frontend and Linux binaries with deploy/build.sh before deploying.
FROM dopplerhq/cli:3
USER root
RUN apk add --no-cache bash ca-certificates && adduser -D -u 10001 app
WORKDIR /app
COPY ephemeral/bin/zoom-connect-linux /app/zoom-connect
COPY ephemeral/bin/rtmsrec-linux /app/rtmsrec
COPY deploy/run-services.sh /app/run-services.sh
RUN mkdir -p /app/recordings && chown -R app:app /app
USER app
ENTRYPOINT ["doppler", "run", "--no-fallback", "--no-check-version", "--"]
CMD ["/app/run-services.sh"]
