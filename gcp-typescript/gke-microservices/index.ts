// GCP GKE Microservices with Pulumi (TypeScript)
// Production GKE cluster with Cloud SQL and Load Balancer

import * as pulumi from "@pulumi/pulumi";
import * as gcp from "@pulumi/gcp";
import * as k8s from "@pulumi/kubernetes";

const config = new pulumi.Config();
const environment = pulumi.getStack();
const project = gcp.config.project!;
const region = gcp.config.region || "us-central1";

// GKE Cluster
const cluster = new gcp.container.Cluster("gke-cluster", {
    name: `gke-cluster-${environment}`,
    location: region,
    
    // Remove default node pool
    removeDefaultNodePool: true,
    initialNodeCount: 1,
    
    // Network configuration
    networkingMode: "VPC_NATIVE",
    ipAllocationPolicy: {
        clusterIpv4CidrBlock: "/16",
        servicesIpv4CidrBlock: "/22",
    },
    
    // Enable Workload Identity
    workloadIdentityConfig: {
        workloadPool: `${project}.svc.id.goog`,
    },
    
    // Enable add-ons
    addonsConfig: {
        httpLoadBalancing: { disabled: false },
        horizontalPodAutoscaling: { disabled: false },
        gcePersistentDiskCsiDriverConfig: { enabled: true },
    },
    
    // Security
    masterAuth: {
        clientCertificateConfig: {
            issueClientCertificate: false,
        },
    },
    
    // Enable binary authorization
    binaryAuthorization: {
        evaluationMode: "PROJECT_SINGLETON_POLICY_ENFORCE",
    },
});

// Node Pool (with spot instances for cost optimization)
const nodePool = new gcp.container.NodePool("primary-node-pool", {
    name: `primary-pool-${environment}`,
    cluster: cluster.name,
    location: region,
    
    nodeCount: 2,
    
    autoscaling: {
        minNodeCount: 1,
        maxNodeCount: 5,
    },
    
    nodeConfig: {
        machineType: "e2-medium",
        
        // Use spot instances
        spot: true,
        
        // Workload Identity
        workloadMetadataConfig: {
            mode: "GKE_METADATA",
        },
        
        oauthScopes: [
            "https://www.googleapis.com/auth/cloud-platform",
        ],
        
        labels: {
            environment: environment,
        },
        
        tags: ["gke-node", environment],
    },
    
    management: {
        autoRepair: true,
        autoUpgrade: true,
    },
});

// Cloud SQL Instance
const dbInstance = new gcp.sql.DatabaseInstance("postgres-instance", {
    name: `postgres-${environment}`,
    region: region,
    databaseVersion: "POSTGRES_15",
    
    settings: {
        tier: "db-f1-micro",
        
        ipConfiguration: {
            ipv4Enabled: true,
            authorizedNetworks: [],
             // Enable private IP
            privateNetwork: undefined, // Would need VPC setup
        },
        
        backupConfiguration: {
            enabled: true,
            startTime: "03:00",
            pointInTimeRecoveryEnabled: true,
        },
        
        maintenanceWindow: {
            day: 7,  // Sunday
            hour: 3,
        },
    },
    
    deletionProtection: environment === "production",
});

// Database
const database = new gcp.sql.Database("app-database", {
    name: "appdb",
    instance: dbInstance.name,
});

// Kubernetes Provider
const k8sProvider = new k8s.Provider("gke-k8s", {
    kubeconfig: pulumi.
        all([cluster.name, cluster.endpoint, cluster.masterAuth]).
        apply(([name, endpoint, masterAuth]) => {
            const context = `${gcp.config.project}_${gcp.config.zone}_${name}`;
            return `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: ${masterAuth.clusterCaCertificate}
    server: https://${endpoint}
  name: ${context}
contexts:
- context:
    cluster: ${context}
    user: ${context}
  name: ${context}
current-context: ${context}
kind: Config
preferences: {}
users:
- name: ${context}
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: gke-gcloud-auth-plugin
      installHint: Install gke-gcloud-auth-plugin for use with kubectl
      provideClusterInfo: true
`;
        }),
});

// Deploy sample application
const appLabels = { app: "nginx" };
const deployment = new k8s.apps.v1.Deployment("nginx", {
    metadata: { name: "nginx" },
    spec: {
        replicas: 2,
        selector: { matchLabels: appLabels },
        template: {
            metadata: { labels: appLabels },
            spec: {
                containers: [{
                    name: "nginx",
                    image: "nginx:latest",
                    ports: [{ containerPort: 80 }],
                }],
            },
        },
    },
}, { provider: k8sProvider });

// Service with LoadBalancer
const service = new k8s.core.v1.Service("nginx", {
    metadata: { name: "nginx" },
    spec: {
        type: "LoadBalancer",
        selector: appLabels,
        ports: [{ port: 80, targetPort: 80 }],
    },
}, { provider: k8sProvider });

// Exports
export const clusterName = cluster.name;
export const kubeconfig = k8sProvider.kubeconfig;
export const clusterEndpoint = cluster.endpoint;
export const dbInstanceName = dbInstance.name;
export const dbConnectionName = dbInstance.connectionName;
export const serviceIP = service.status.loadBalancer.ingress[0].ip;
