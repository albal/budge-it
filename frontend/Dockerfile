# Build stage: Red Hat UBI Node.js toolset
FROM registry.access.redhat.com/ubi9/nodejs-20:latest AS build
WORKDIR /opt/app-root/src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

# Runtime stage: UBI nginx (listens on 8080, runs as non-root by default,
# restricted SCC compatible). The default server includes any *.conf dropped
# into /opt/app-root/etc/nginx.default.d/.
FROM registry.access.redhat.com/ubi9/nginx-124:latest
COPY --from=build /opt/app-root/src/dist/ /opt/app-root/src/
COPY nginx-default.conf /opt/app-root/etc/nginx.default.d/budgeit.conf
EXPOSE 8080
CMD ["nginx", "-g", "daemon off;"]
