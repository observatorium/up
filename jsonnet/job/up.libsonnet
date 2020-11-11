// These are the defaults for this components configuration.
// When calling the function to generate the component's manifest,
// you can pass an object structured like the default to overwrite default values.
local defaults = {
  local defaults = self,
  name: error 'must provide name',
  namespace: error 'must provide namespace',
  version: error 'must provide version',
  image: error 'must provide image',
  endpointType: error 'must provide endpoint type',
  writeEndpoint: error 'must provide writeEndpoint',
  readEndpoint: error 'must provide readEndpoint',
  backoffLimit: error 'must provide backoffLimit',
  replicas: 1,
  tls: {},
  resources: {},
  getToken: {},
  sendLogs: {},

  commonLabels:: {
    'app.kubernetes.io/name': 'observatorium-up',
    'app.kubernetes.io/instance': defaults.name,
    'app.kubernetes.io/version': defaults.version,
    'app.kubernetes.io/component': 'test',
  },
};

function(params) {
  local up = self,

  // Combine the defaults and the passed params to make the component's config.
  config:: defaults + params,
  // Safety checks for combined config of defaults and params
  assert std.isNumber(up.config.replicas) && up.config.replicas >= 0 : 'observatorium up job replicas has to be number >= 0',
  assert std.isObject(up.config.resources),
  assert std.isObject(up.config.tls),

  job:
    local bash = {
      name: 'logs-file',
      image: up.config.sendLogs.image,
      command: [
        '/bin/sh',
        '-c',
        |||
          cat > /var/logs-file/logs.yaml << EOF
          spec:
            logs: [ [ "$(date '+%s%N')", "log line"] ]
          EOF
        |||,
      ],
      volumeMounts: [
        { name: 'logs-file', mountPath: '/var/logs-file', readOnly: false },
      ],
    };
    local curl = {
      name: 'curl',
      image: up.config.getToken.image,
      command: [
        '/bin/sh',
        '-c',
        |||
          curl --request POST \
              --silent \
              %s \
              --url %s \
              --header 'content-type: application/x-www-form-urlencoded' \
              --data grant_type=password \
              --data username=%s \
              --data password=%s \
              --data client_id=%s \
              --data client_secret=%s \
              --data scope="openid email" | sed 's/^{.*"id_token":[^"]*"\([^"]*\)".*}/\1/' > /var/shared/token
        ||| % [
          (if std.objectHas(up.config.getToken, 'oidc') then '--cacert /mnt/oidc-tls/%s' % [up.config.getToken.oidc.caKey] else ''),
          up.config.getToken.endpoint,
          up.config.getToken.username,
          up.config.getToken.password,
          up.config.getToken.clientID,
          up.config.getToken.clientSecret,
        ],
      ],
      volumeMounts:
        [{ name: 'shared', mountPath: '/var/shared', readOnly: false }] +
        (if std.objectHas(up.config.getToken, 'oidc') then [{ name: 'oidc-tls', mountPath: '/mnt/oidc-tls', readOnly: true }] else []),
    };
    local c = {
      name: 'observatorium-up',
      image: up.config.image,
      args: [
              '--endpoint-type=' + up.config.endpointType,
              '--endpoint-write=' + up.config.writeEndpoint,
              '--endpoint-read=' + up.config.readEndpoint,
              '--period=1s',
              '--duration=2m',
              '--name=foo',
              '--labels=bar="baz"',
              '--latency=10s',
              '--initial-query-delay=5s',
              '--threshold=0.90',
            ] +
            (if up.config.tls != {} then ['--tls-ca-file=/mnt/tls/' + up.config.tls.caKey] else []) +
            (if up.config.getToken != {} then ['--token-file=/var/shared/token'] else []) +
            (if up.config.sendLogs != {} then ['--logs-file=/var/logs-file/logs.yaml'] else []),
      volumeMounts:
        (if up.config.tls != {} then [{ name: 'tls', mountPath: '/mnt/tls', readOnly: true }] else []) +
        (if up.config.getToken != {} then [{ name: 'shared', mountPath: '/var/shared', readOnly: true }] else []) +
        (if up.config.sendLogs != {} then [{ name: 'logs-file', mountPath: '/var/logs-file', readOnly: true }] else []),
      resources: if up.config.resources != {} then up.config.resources else {},
    };
    {
      apiVersion: 'batch/v1',
      kind: 'Job',
      metadata: {
        name: up.config.name,
        labels: up.config.commonLabels,
      },
      spec: {
        backoffLimit: up.config.backoffLimit,
        template: {
          metadata: {
            labels: up.config.commonLabels,
          },
          spec: {
            initContainers+:
              (if up.config.getToken != {} then [curl] else []) +
              (if up.config.sendLogs != {} then [bash] else []),
            containers: [c],
            restartPolicy: 'OnFailure',
            volumes:
              (if up.config.tls != {} then [{ configMap: { name: up.config.tls.configMapName }, name: 'tls' }] else []) +
              (if up.config.getToken != {} then [{ emptyDir: {}, name: 'shared' }] else []) +
              (if up.config.sendLogs != {} then [{ emptyDir: {}, name: 'logs-file' }] else []) +
              (if std.objectHas(up.config.getToken, 'oidc') then [{ configMap: { name: up.config.getToken.oidc.configMapName }, name: 'oidc-tls' }] else []),
          },
        },
      },
    },
}
