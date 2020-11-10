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
  replicas: error 'must provide replicas',
  queryConfig: {},
  readEndpoint: '',
  writeEndpoint: '',
  logs: '',
  ports: { http: 8080 },
  resources: {},
  serviceMonitor: false,

  commonLabels:: {
    'app.kubernetes.io/name': 'observatorium-up',
    'app.kubernetes.io/instance': defaults.name,
    'app.kubernetes.io/version': defaults.version,
    'app.kubernetes.io/component': 'blackbox-prober',
  },

  podLabelSelector:: {
    [labelName]: defaults.commonLabels[labelName]
    for labelName in std.objectFields(defaults.commonLabels)
    if !std.setMember(labelName, ['app.kubernetes.io/version'])
  },
};

function(params) {
  local up = self,

  // Combine the defaults and the passed params to make the component's config.
  config:: defaults + params,
  // Safety checks for combined config of defaults and params
  assert std.isNumber(up.config.replicas) && up.config.replicas >= 0 : 'observatorium up replicas has to be number >= 0',
  assert std.isObject(up.config.resources),
  assert std.isObject(up.config.queryConfig),
  assert std.isBoolean(up.config.serviceMonitor),

  service: {
    apiVersion: 'v1',
    kind: 'Service',
    metadata: {
      name: up.config.name,
      namespace: up.config.namespace,
      labels: up.config.commonLabels,
    },
    spec: {
      ports: [
        {
          assert std.isString(name),
          assert std.isNumber(up.config.ports[name]),

          name: name,
          port: up.config.ports[name],
          targetPort: up.config.ports[name],
        }
        for name in std.objectFields(up.config.ports)
      ],
      selector: up.config.podLabelSelector,
    },
  },

  deployment:
    local c = {
      name: 'observatorium-up',
      image: up.config.image,
      args: [
              '--duration=0',
              '--log.level=debug',
              '--endpoint-type=' + up.config.endpointType,
            ] +
            (if up.config.queryConfig != {} then ['--queries-file=/etc/up/queries.yaml'] else []) +
            (if up.config.readEndpoint != '' then ['--endpoint-read=' + up.config.readEndpoint] else []) +
            (if up.config.writeEndpoint != '' then ['--endpoint-write=' + up.config.writeEndpoint] else []) +
            (if up.config.logs != '' then ['--logs=' + up.config.logs] else []),
      ports: [
        { name: port.name, containerPort: port.port }
        for port in up.service.spec.ports
      ],
      volumeMounts: if up.config.queryConfig != {} then [
        { mountPath: '/etc/up/', name: 'query-config', readOnly: false },
      ] else [],
      resources: if up.config.resources != {} then up.config.resources else {},
    };

    {
      apiVersion: 'apps/v1',
      kind: 'Deployment',
      metadata: {
        name: up.config.name,
        namespace: up.config.namespace,
        labels: up.config.commonLabels,
      },
      spec: {
        replicas: up.config.replicas,
        selector: { matchLabels: up.config.podLabelSelector },
        template: {
          metadata: {
            labels: up.config.commonLabels,
          },
          spec: {
            containers: [c],
            volumes: if up.config.queryConfig != {} then
              [{ configMap: { name: up.config.name }, name: 'query-config' }]
            else [],
          },
        },
      },
    },

  configmap: if up.config.queryConfig != {} then {
    apiVersion: 'v1',
    data: {
      'queries.yaml': std.manifestYamlDoc(up.config.queryConfig),
    },
    kind: 'ConfigMap',
    metadata: {
      labels: up.config.commonLabels,
      name: up.config.name,
      namespace: up.config.namespace,
    },
  } else null,

  serviceMonitor: if up.config.serviceMonitor == true then {
    apiVersion: 'monitoring.coreos.com/v1',
    kind: 'ServiceMonitor',
    metadata+: {
      name: up.config.name,
      namespace: up.config.namespace,
    },
    spec: {
      selector: {
        matchLabels: up.config.podLabelSelector,
      },
      endpoints: [
        { port: 'http' },
      ],
    },
  } else null,
}
