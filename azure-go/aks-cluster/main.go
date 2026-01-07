// Azure AKS Infrastructure with Pulumi (Go)
// Production AKS cluster with Azure KeyVault and CosmosDB

package main

import (
	"github.com/pulumi/pulumi-azure-native-sdk/containerservice/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/documentdb/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/keyvault/v2"
	"github.com/pulumi/pulumi-azure-native-sdk/resources/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		location := cfg.Get("location")
		if location == "" {
			location = "eastus"
		}
		environment := ctx.Stack()

		// Resource Group
		resourceGroup, err := resources.NewResourceGroup(ctx, "rg", &resources.ResourceGroupArgs{
			ResourceGroupName: pulumi.Sprintf("rg-aks-%s", environment),
			Location:          pulumi.String(location),
			Tags: pulumi.StringMap{
				"Environment": pulumi.String(environment),
				"ManagedBy":   pulumi.String("Pulumi"),
			},
		})
		if err != nil {
			return err
		}

		// AKS Cluster
		cluster, err := containerservice.NewManagedCluster(ctx, "aksCluster", &containerservice.ManagedClusterArgs{
			ResourceGroupName: resourceGroup.Name,
			ResourceName:      pulumi.Sprintf("aks-%s", environment),
			Location:          resourceGroup.Location,
			
			KubernetesVersion: pulumi.String("1.28.0"),
			DnsPrefix:         pulumi.Sprintf("aks-%s", environment),
			
			Identity: &containerservice.ManagedClusterIdentityArgs{
				Type: containerservice.ResourceIdentityTypeSystemAssigned,
			},
			
			// Agent Pools
			AgentPoolProfiles: containerservice.ManagedClusterAgentPoolProfileArray{
				&containerservice.ManagedClusterAgentPoolProfileArgs{
					Name:              pulumi.String("systempool"),
					Count:             pulumi.Int(2),
					VmSize:            pulumi.String("Standard_D2s_v3"),
					OsDiskSizeGB:      pulumi.Int(30),
					Mode:              pulumi.String("System"),
					EnableAutoScaling: pulumi.Bool(true),
					MinCount:          pulumi.Int(1),
					MaxCount:          pulumi.Int(5),
					Type:              pulumi.String("VirtualMachineScaleSets"),
				},
				&containerservice.ManagedClusterAgentPoolProfileArgs{
					Name:              pulumi.String("userpool"),
					Count:             pulumi.Int(2),
					VmSize:            pulumi.String("Standard_D2s_v3"),
					OsDiskSizeGB:      pulumi.Int(30),
					Mode:              pulumi.String("User"),
					EnableAutoScaling: pulumi.Bool(true),
					MinCount:          pulumi.Int(0),
					MaxCount:          pulumi.Int(10),
					// Spot instances for cost optimization
					ScaleSetPriority: pulumi.String("Spot"),
					SpotMaxPrice:     pulumi.Float64(-1),
					Type:             pulumi.String("VirtualMachineScaleSets"),
				},
			},
			
			// Network Configuration
			NetworkProfile: &containerservice.ContainerServiceNetworkProfileArgs{
				NetworkPlugin: pulumi.String("azure"),
				ServiceCidr:   pulumi.String("10.0.0.0/16"),
				DnsServiceIP:  pulumi.String("10.0.0.10"),
			},
			
			// Add-ons
			AddonProfiles: containerservice.ManagedClusterAddonProfileMap{
				"azureKeyvaultSecretsProvider": &containerservice.ManagedClusterAddonProfileArgs{
					Enabled: pulumi.Bool(true),
				},
				"omsagent": &containerservice.ManagedClusterAddonProfileArgs{
					Enabled: pulumi.Bool(true),
				},
			},
			
			Tags: pulumi.StringMap{
				"Environment": pulumi.String(environment),
				"ManagedBy":   pulumi.String("Pulumi"),
			},
		})
		if err != nil {
			return err
		}

		// Azure Key Vault
		vault, err := keyvault.NewVault(ctx, "keyVault", &keyvault.VaultArgs{
			ResourceGroupName: resourceGroup.Name,
			VaultName:         pulumi.Sprintf("kv-%s", environment),
			Location:          resourceGroup.Location,
			
			Properties: &keyvault.VaultPropertiesArgs{
				TenantId: pulumi.String("TENANT_ID"), // Replace with actual tenant ID
				Sku: &keyvault.SkuArgs{
					Family: pulumi.String("A"),
					Name:   keyvault.SkuNameStandard,
				},
				EnabledForDeployment:         pulumi.Bool(true),
				EnabledForDiskEncryption:     pulumi.Bool(true),
				EnabledForTemplateDeployment: pulumi.Bool(true),
				EnableSoftDelete:             pulumi.Bool(true),
				SoftDeleteRetentionInDays:    pulumi.Int(90),
				EnablePurgeProtection:        pulumi.Bool(true),
			},
			
			Tags: pulumi.StringMap{
				"Environment": pulumi.String(environment),
			},
		})
		if err != nil {
			return err
		}

		// Cosmos DB Account
		cosmosAccount, err := documentdb.NewDatabaseAccount(ctx, "cosmosAccount", &documentdb.DatabaseAccountArgs{
			ResourceGroupName: resourceGroup.Name,
			AccountName:       pulumi.Sprintf("cosmos-%s", environment),
			Location:          resourceGroup.Location,
			
			DatabaseAccountOfferType: pulumi.String("Standard"),
			
			Locations: documentdb.LocationArray{
				&documentdb.LocationArgs{
					LocationName:     resourceGroup.Location,
					FailoverPriority: pulumi.Int(0),
				},
			},
			
			ConsistencyPolicy: &documentdb.ConsistencyPolicyArgs{
				DefaultConsistencyLevel: documentdb.DefaultConsistencyLevelSession,
			},
			
			// Enable automatic failover
			EnableAutomaticFailover: pulumi.Bool(true),
			
			// Backup policy
			BackupPolicy: &documentdb.ContinuousModeBackupPolicyArgs{
				Type: pulumi.String("Continuous"),
			},
			
			Tags: pulumi.StringMap{
				"Environment": pulumi.String(environment),
			},
		})
		if err != nil {
			return err
		}

		// Exports
		ctx.Export("resourceGroupName", resourceGroup.Name)
		ctx.Export("aksClusterName", cluster.Name)
		ctx.Export("keyVaultName", vault.Name)
		ctx.Export("cosmosAccountName", cosmosAccount.Name)
		
		// Kubeconfig
		ctx.Export("kubeconfig", pulumi.All(cluster.Name, resourceGroup.Name).ApplyT(
			func(args []interface{}) (string, error) {
				clusterName := args[0].(string)
				rgName := args[1].(string)
				return pulumi.Sprintf("az aks get-credentials --resource-group %s --name %s", rgName, clusterName).StringValue(), nil
			},
		))

		return nil
	})
}
