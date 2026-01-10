# external-dns-pfsense-webhook

external-dns args:

```yaml
- --provider=webhook
- --webhook-provider-url=http://localhost:8888
- --managed-record-types=A
- --registry=txt
- --txt-prefix=%{record_type}-prefix-
- --regex-domain-filter='^.*$'
- --regex-domain-exclusion='^\*\..*'
```

webhook sidecar:

```yaml
- name: pfsense
  image: ghcr.io/slamdev/external-dns-pfsense-webhook:latest
  ports:
    - name: http
      containerPort: 8888
    - name: monitoring
      containerPort: 8080
  resources:
    requests:
      cpu: 10m
      memory: 50Mi
  env:
    - name: APP_PFSENSE_URL
      value: https://example.com
    - name: APP_PFSENSE_INSECURE
      value: "true"
    - name: APP_PFSENSE_USERNAME
      value: admin
    - name: APP_PFSENSE_PASSWORD
      value: admin
    - name: APP_DRYRUN
      value: "false"
  startupProbe:
    httpGet:
      path: /ready
      port: monitoring
  livenessProbe:
    httpGet:
      path: /live
      port: monitoring
  readinessProbe:
    httpGet:
      path: /ready
      port: monitoring
```

Wildcard domains are excluded cause pfsence relies on custom unbound options to support them, this controller doesn't
manage those.

To manage wildcard domains you can add custom unbound options like:

```yaml
server:
  local-zone: "sub.exmaple.com" redirect
  local-data: "sub.exmaple.com 3600 IN A 10.1.10.1"
```

This will create a wildcard A record for `*.sub.example.com` pointing to `10.1.10.1`.

Unbound record description is used to store external-dns metadata. Metadata is converted to JSON and then base64
encoded. Encoding is required because unbound (or pfsense) sometimes converts `"` to `&quot;` which breaks JSON parsing.
