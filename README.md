# external-dns-pfsense-webhook

external-dns args:

```yaml
- --provider=webhook
- --webhook-provider-url=http://localhost:8888
- --managed-record-types=A
- --registry=txt
- --txt-prefix=%{record_type}-prefix-
- --txt-wildcard-replacement=wildcard
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
