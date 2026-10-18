import { createServer } from "node:http";

const coreResources = [
  ["pods", "Pod", true],
  ["services", "Service", true],
  ["endpoints", "Endpoints", true],
  ["configmaps", "ConfigMap", true],
  ["secrets", "Secret", true],
  ["persistentvolumeclaims", "PersistentVolumeClaim", true],
  ["persistentvolumes", "PersistentVolume", false],
  ["nodes", "Node", false],
  ["namespaces", "Namespace", false],
  ["events", "Event", true],
];
const groupedResources = {
  "apps/v1": [
    ["deployments", "Deployment", true],
    ["replicasets", "ReplicaSet", true],
    ["statefulsets", "StatefulSet", true],
    ["daemonsets", "DaemonSet", true],
  ],
  "batch/v1": [
    ["jobs", "Job", true],
    ["cronjobs", "CronJob", true],
  ],
  "networking.k8s.io/v1": [["ingresses", "Ingress", true]],
  "storage.k8s.io/v1": [["storageclasses", "StorageClass", false]],
};

const apiVersionFor = (group, version) => group ? `${group}/${version}` : version;
const listKind = (kind) => `${kind}List`;
const clone = (value) => JSON.parse(JSON.stringify(value));

function metadata(name, namespace, labels = {}, annotations = {}) {
  return {
    name,
    namespace,
    uid: `uid-${namespace || "cluster"}-${name}`,
    resourceVersion: "1",
    creationTimestamp: "2024-02-03T04:05:06Z",
    labels,
    annotations,
  };
}

function pod(name, namespace, tier, phase = "Running", waitingReason = "") {
  const waiting = waitingReason || (phase === "Running" ? "" : phase);
  const running = phase === "Running" && !waiting;
  return {
    apiVersion: "v1",
    kind: "Pod",
    metadata: metadata(name, namespace, { app: name, tier, team: "platform" }, { owner: "ui-test" }),
    spec: {
      nodeName: "node-a",
      containers: [
        { name: "app", image: "example.invalid/ui-test:1" },
        { name: "sidecar", image: "example.invalid/ui-test-sidecar:1" },
      ],
    },
    status: {
      phase,
      podIP: "10.0.0.10",
      containerStatuses: [
        { name: "app", ready: running, restartCount: 0, state: running ? { running: { startedAt: "2024-02-03T04:05:06Z" } } : { waiting: { reason: waiting } } },
        { name: "sidecar", ready: phase === "Running", restartCount: 1, state: phase === "Running" ? { running: { startedAt: "2024-02-03T04:05:06Z" } } : { waiting: { reason: waiting } } },
      ],
    },
  };
}

function deployment(name, replicas, available = replicas) {
  const annotations = { owner: "ui-test" };
  if (name === "api-gateway") annotations["deployment.kubernetes.io/revision"] = "2";
  return {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: metadata(name, "dev", { app: name, tier: name === "api-gateway" ? "frontend" : "backend" }, annotations),
    spec: {
      replicas,
      selector: { matchLabels: { app: name } },
      template: { metadata: { labels: { app: name } }, spec: { containers: [{ name: "app", image: "example.invalid/ui-test:1" }] } },
    },
    status: {
      replicas,
      readyReplicas: available,
      updatedReplicas: replicas,
      availableReplicas: available,
      conditions: [{ type: "Available", status: available === replicas ? "True" : "False", reason: "FixtureState", message: "Deterministic fixture status" }],
    },
  };
}

function replicaSet(name, revision, replicas, image) {
  return {
    apiVersion: "apps/v1",
    kind: "ReplicaSet",
    metadata: {
      ...metadata(name, "dev", { app: "api-gateway" }),
      annotations: { "deployment.kubernetes.io/revision": String(revision) },
      ownerReferences: [{ apiVersion: "apps/v1", kind: "Deployment", name: "api-gateway", uid: "uid-dev-api-gateway", controller: true, blockOwnerDeletion: true }],
    },
    spec: {
      replicas,
      selector: { matchLabels: { app: "api-gateway" } },
      template: { metadata: { labels: { app: "api-gateway" } }, spec: { containers: [{ name: "app", image }] } },
    },
  };
}

function event(name, type, reason, message) {
  return {
    apiVersion: "v1",
    kind: "Event",
    metadata: metadata(name, "dev"),
    type,
    reason,
    message,
    involvedObject: { kind: "Pod", name: "alpha", namespace: "dev" },
    firstTimestamp: "2024-02-03T04:05:06Z",
    lastTimestamp: "2024-02-03T04:05:06Z",
    count: 1,
  };
}

function initialObjects() {
  return {
    pods: [
      pod("alpha", "dev", "frontend"),
      pod("bravo", "dev", "backend"),
      pod("conflict", "dev", "frontend"),
      pod("invalid-metadata", "dev", "backend"),
      pod("crashy", "dev", "backend", "Running", "CrashLoopBackOff"),
      pod("registry-unavailable", "dev", "backend", "Running", "RegistryUnavailable"),
      pod("pending-pod", "dev", "backend", "Pending"),
      pod("failed-pod", "dev", "backend", "Failed"),
      pod("qa-only", "qa", "test"),
    ],
    deployments: [deployment("api-gateway", 3, 2), deployment("worker", 2, 2), deployment("conflict-deployment", 1, 1), deployment("invalid-scale", 1, 1)],
    replicasets: [
      replicaSet("api-gateway-v2", 2, 3, "example.invalid/ui-test:2"),
      replicaSet("api-gateway-v1", 1, 2, "example.invalid/ui-test:1"),
    ],
    daemonsets: [],
    jobs: [],
    cronjobs: [],
    services: [{ apiVersion: "v1", kind: "Service", metadata: metadata("frontend", "dev", { app: "api-gateway" }), spec: { type: "ClusterIP", clusterIP: "10.96.0.10", ports: [{ port: 80, targetPort: 8080 }] } }],
    ingresses: [],
    endpoints: [],
    configmaps: [],
    secrets: [],
    persistentvolumeclaims: [],
    persistentvolumes: [],
    storageclasses: [],
    nodes: [{ apiVersion: "v1", kind: "Node", metadata: metadata("node-a", "", { "kubernetes.io/hostname": "node-a" }), spec: {}, status: { capacity: { cpu: "4", memory: "8Gi" }, allocatable: { cpu: "4", memory: "8Gi" }, conditions: [{ type: "Ready", status: "True", reason: "FixtureReady", message: "Ready" }] } }],
    namespaces: [
      { apiVersion: "v1", kind: "Namespace", metadata: metadata("dev", "", { purpose: "tests" }), status: { phase: "Active" } },
      { apiVersion: "v1", kind: "Namespace", metadata: metadata("qa", "", { purpose: "tests" }), status: { phase: "Active" } },
    ],
    events: [event("alpha-started", "Normal", "Started", "Container app started"), event("alpha-warning", "Warning", "BackOff", "Container app is waiting")],
  };
}

function apiResourceList(groupVersion, defs) {
  return {
    apiVersion: "v1",
    kind: "APIResourceList",
    groupVersion,
    resources: defs.map(([name, kind, namespaced]) => ({
      name,
      singularName: name.replace(/s$/, ""),
      namespaced,
      kind,
      verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
    })),
  };
}

function sendJSON(response, statusCode, payload) {
  response.writeHead(statusCode, { "content-type": "application/json" });
  response.end(JSON.stringify(payload));
}

function statusObject(code, reason, message, name = "") {
  return {
    apiVersion: "v1",
    kind: "Status",
    status: "Failure",
    message,
    reason,
    details: name ? { name } : undefined,
    code,
  };
}

function mergeObject(target, patch) {
  if (!patch || typeof patch !== "object" || Array.isArray(patch)) return target;
  for (const [key, value] of Object.entries(patch)) {
    if (value === null) delete target[key];
    else if (value && typeof value === "object" && !Array.isArray(value)) {
      target[key] = mergeObject(target[key] && typeof target[key] === "object" ? target[key] : {}, value);
    } else target[key] = value;
  }
  return target;
}

function selectorMatches(object, raw, field) {
  if (!raw) return true;
  return raw.split(",").every((part) => {
    const match = /^\s*([^!=]+?)\s*(==|=|!=)\s*(.*?)\s*$/.exec(part);
    if (!match) return true;
    const [, key, operator, expected] = match;
    let actual;
    if (field) {
      actual = key === "metadata.name" ? object.metadata?.name : key === "metadata.namespace" ? object.metadata?.namespace : key.split(".").reduce((value, segment) => value?.[segment], object);
    } else actual = object.metadata?.labels?.[key];
    return operator === "!=" ? String(actual ?? "") !== expected : String(actual ?? "") === expected;
  });
}

function resourceDefs(group, version) {
  if (!group) return version === "v1" ? coreResources : [];
  return groupedResources[`${group}/${version}`] || [];
}

function requestResource(pathname) {
  const parts = pathname.split("/").filter(Boolean).map(decodeURIComponent);
  let group = "";
  let version = "";
  let rest;
  if (parts[0] === "api") {
    version = parts[1] || "";
    rest = parts.slice(2);
  } else if (parts[0] === "apis") {
    group = parts[1] || "";
    version = parts[2] || "";
    rest = parts.slice(3);
  } else return null;

  let namespace = "";
  if (rest[0] === "namespaces") {
    if (rest.length === 1) return { group, version, resource: "namespaces", name: "", namespace: "" };
    namespace = rest[1] || "";
    if (rest.length === 2) return { group, version, resource: "namespaces", name: namespace, namespace: "" };
    return { group, version, resource: rest[2] || "", name: rest[3] || "", subresource: rest[4] || "", namespace };
  }
  return { group, version, resource: rest[0] || "", name: rest[1] || "", subresource: rest[2] || "", namespace };
}

export async function startFakeKubernetes() {
  const objects = initialObjects();
  const controls = { failNextLog: false, closeNextWatch: false };
  const requests = [];
  const watches = { started: 0, closed: 0 };
  const server = createServer(async (request, response) => {
    const url = new URL(request.url || "/", "http://127.0.0.1");
    const record = { method: request.method || "GET", path: url.pathname, query: Object.fromEntries(url.searchParams.entries()), body: undefined };
    requests.push(record);

    if (request.method === "GET" && url.pathname === "/api") {
      sendJSON(response, 200, { apiVersion: "v1", kind: "APIVersions", versions: ["v1"], serverAddressByClientCIDRs: [] });
      return;
    }
    if (request.method === "GET" && url.pathname === "/apis") {
      const groups = [...new Set(Object.keys(groupedResources).map((key) => key.split("/")[0]))].map((name) => ({
        name,
        versions: [{ groupVersion: `${name}/v1`, version: "v1" }],
        preferredVersion: { groupVersion: `${name}/v1`, version: "v1" },
      }));
      sendJSON(response, 200, { apiVersion: "v1", kind: "APIGroupList", groups });
      return;
    }
    if (request.method === "GET" && url.pathname === "/api/v1") {
      sendJSON(response, 200, apiResourceList("v1", coreResources));
      return;
    }
    const groupRoot = /^\/apis\/([^/]+)$/.exec(url.pathname);
    if (request.method === "GET" && groupRoot) {
      const group = decodeURIComponent(groupRoot[1]);
      sendJSON(response, 200, {
        apiVersion: "v1",
        kind: "APIGroup",
        name: group,
        versions: [{ groupVersion: `${group}/v1`, version: "v1" }],
        preferredVersion: { groupVersion: `${group}/v1`, version: "v1" },
      });
      return;
    }
    const groupVersionPath = /^\/apis\/([^/]+)\/([^/]+)$/.exec(url.pathname);
    if (request.method === "GET" && groupVersionPath) {
      const key = `${decodeURIComponent(groupVersionPath[1])}/${decodeURIComponent(groupVersionPath[2])}`;
      sendJSON(response, 200, apiResourceList(key, groupedResources[key] || []));
      return;
    }

    const resourcePath = requestResource(url.pathname);
    if (!resourcePath || !resourceDefs(resourcePath.group, resourcePath.version).some(([name]) => name === resourcePath.resource)) {
      sendJSON(response, 404, statusObject(404, "NotFound", `No resource at ${url.pathname}`));
      return;
    }
    const { resource, name, namespace, subresource } = resourcePath;
    if (resource === "namespaces" && !name && request.method === "GET") {
      return sendJSON(response, 200, { apiVersion: "v1", kind: "NamespaceList", metadata: { resourceVersion: "17" }, items: clone(objects.namespaces) });
    }

    if (url.searchParams.get("watch") === "true" && request.method === "GET") {
      watches.started++;
      const closeImmediately = controls.closeNextWatch;
      controls.closeNextWatch = false;
      response.writeHead(200, { "content-type": "application/json", "cache-control": "no-cache" });
      // The watch starts at the list resourceVersion; replaying existing objects here creates an impossible initial update.
      let active = true;
      response.on("close", () => {
        if (active) {
          active = false;
          watches.closed++;
        }
      });
      if (closeImmediately) setImmediate(() => response.destroy());
      return;
    }

    if (resource === "pods" && subresource === "log" && request.method === "GET") {
      record.logOptions = Object.fromEntries(url.searchParams.entries());
      if (controls.failNextLog) {
        controls.failNextLog = false;
        return sendJSON(response, 500, statusObject(500, "InternalError", "deterministic log stream failure"));
      }
      response.writeHead(200, { "content-type": "text/plain; charset=utf-8", "cache-control": "no-cache" });
      response.write("2024-02-03T04:05:06Z alpha started\nsecond deterministic log line\n");
      return;
    }

    if (request.method === "GET" && !name) {
      let items = (objects[resource] || []).filter((candidate) => !namespace || !candidate.metadata.namespace || candidate.metadata.namespace === namespace);
      items = items.filter((candidate) => selectorMatches(candidate, url.searchParams.get("labelSelector") || "", false));
      items = items.filter((candidate) => selectorMatches(candidate, url.searchParams.get("fieldSelector") || "", true));
      const limit = Number.parseInt(url.searchParams.get("limit") || "0", 10);
      const offset = url.searchParams.get("continue") === "page-2" ? limit : 0;
      const pageItems = limit > 0 ? items.slice(offset, offset + limit) : items;
      const next = limit > 0 && offset + pageItems.length < items.length ? "page-2" : "";
      return sendJSON(response, 200, {
        apiVersion: apiVersionFor(resourcePath.group, resourcePath.version),
        kind: listKind((resourceDefs(resourcePath.group, resourcePath.version).find(([key]) => key === resource) || ["", resource])[1]),
        metadata: { resourceVersion: "17", continue: next },
        items: clone(pageItems),
      });
    }

    const target = (objects[resource] || []).find((candidate) => candidate.metadata.name === name && (!namespace || !candidate.metadata.namespace || candidate.metadata.namespace === namespace));
    if (!target) return sendJSON(response, 404, statusObject(404, "NotFound", `${resource} ${name} not found`, name));
    if (request.method === "GET" && name) {
      const found = clone(target);
      if (subresource === "scale") {
        return sendJSON(response, 200, { apiVersion: "autoscaling/v1", kind: "Scale", metadata: clone(target.metadata), spec: { replicas: target.spec?.replicas ?? 0 }, status: { replicas: target.status?.replicas ?? 0, selector: "app=fixture" } });
      }
      return sendJSON(response, 200, found);
    }

    if (["PUT", "PATCH", "POST"].includes(request.method || "")) {
      let body = "";
      for await (const chunk of request) body += chunk;
      try { record.body = body ? JSON.parse(body) : {}; } catch { record.body = body; }
      if (name === "conflict" || name === "conflict-deployment") {
        return sendJSON(response, 409, statusObject(409, "Conflict", `resource ${name} changed; retry with the current resource version`, name));
      }
      if (name === "invalid-metadata" && /label|annotation/i.test(body)) {
        return sendJSON(response, 422, statusObject(422, "Invalid", "metadata key is invalid", name));
      }

      if (resource === "deployments" && name === "invalid-scale" && record.body?.spec?.replicas !== undefined) {
        return sendJSON(response, 422, statusObject(422, "Invalid", "fixture rejects scaling this Deployment", name));
      }
      if (subresource === "scale") {
        const replicas = record.body?.spec?.replicas;
        if (!Number.isInteger(replicas) || replicas < 0 || replicas > 100000) {
          return sendJSON(response, 422, statusObject(422, "Invalid", "replicas must be an integer between 0 and 100000", name));
        }
        if (name === "invalid-scale") {
          return sendJSON(response, 422, statusObject(422, "Invalid", "fixture rejects scaling this Deployment", name));
        }
        target.spec ||= {};
        target.status ||= {};
        target.spec.replicas = replicas;
        target.status.replicas = replicas;
        target.status.readyReplicas = Math.min(target.status.readyReplicas || 0, replicas);
        target.status.availableReplicas = Math.min(target.status.availableReplicas || 0, replicas);
      } else if (request.method === "PUT" && record.body?.metadata?.name) {
        const index = objects[resource].indexOf(target);
        objects[resource][index] = clone(record.body);
      } else if (Array.isArray(record.body)) {
        for (const operation of record.body) {
          const parts = String(operation.path || "").split("/").slice(1).map((segment) => segment.replaceAll("~1", "/").replaceAll("~0", "~"));
          let cursor = target;
          for (const segment of parts.slice(0, -1)) cursor = cursor[segment] ||= {};
          if (parts.length) cursor[parts.at(-1)] = operation.value;
        }
      } else mergeObject(target, record.body);

      target.metadata.resourceVersion = String(Number(target.metadata.resourceVersion || 1) + 1);
      if (subresource === "scale") {
        return sendJSON(response, 200, { apiVersion: "autoscaling/v1", kind: "Scale", metadata: clone(target.metadata), spec: { replicas: target.spec.replicas }, status: { replicas: target.status.replicas, selector: "app=fixture" } });
      }
      return sendJSON(response, 200, clone(target));
    }
    if (request.method === "DELETE") {
      const index = objects[resource].indexOf(target);
      objects[resource].splice(index, 1);
      return sendJSON(response, 200, { apiVersion: "v1", kind: "Status", status: "Success", code: 200, details: { name } });
    }
    sendJSON(response, 405, statusObject(405, "MethodNotAllowed", `Method ${request.method} is not supported`));
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  return {
    url: `http://127.0.0.1:${address.port}`,
    objects,
    requests,
    controls,
    watches,
    async close() {
      server.closeAllConnections();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}
