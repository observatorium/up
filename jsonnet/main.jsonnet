local commonConfig = {
  local cfg = self,
  namespace: 'observatorium',
  name: 'observatorium-up',
  version: 'master-2020-06-03-8a20b4e',
  image: 'quay.io/observatorium/up:' + cfg.version,
  replicas: 1,
  endpointType: 'metrics',
  writeEndpoint: 'http://FAKE.svc.cluster.local:8080/api/metrics/v1/test/api/v1/receive',
  readEndpoint: 'http://FAKE.svc.cluster.local:8080/api/metrics/v1/test/api/v1/query',
};

local up = (import 'up.libsonnet')(commonConfig);
local job = (import 'job/up.libsonnet')(commonConfig { backoffLimit: 5 });
local jobWithGetToken = (import 'job/up.libsonnet')(commonConfig {
  backoffLimit: 5,
  getToken: {
    image: 'docker.io/curlimages/curl',
    endpoint: 'http://FAKE.svc.cluster.local:%d/dex/token',
    username: 'admin@example.com',
    password: 'password',
    clientID: 'test',
    clientSecret: 'ZXhhbXBsZS1hcHAtc2VjcmV0',
  },
});
local jobWithLogs = (import 'job/up.libsonnet')(commonConfig {
  backoffLimit: 5,
  sendLogs: {
    // Note: Keep debian here because we need coreutils' date
    // for timestamp generation in nanoseconds.
    image: 'docker.io/debian',
  },
});

{ ['up-' + name]: up[name] for name in std.objectFields(up) if up[name] != null } +
{ ['up-' + name]: job[name] for name in std.objectFields(job) if job[name] != null } +
{ ['up-%s-with-get-token' % name]: jobWithGetToken[name] for name in std.objectFields(jobWithGetToken) if jobWithGetToken[name] != null } +
{ ['up-%s-with-logs' % name]: jobWithLogs[name] for name in std.objectFields(jobWithLogs) if jobWithLogs[name] != null }
