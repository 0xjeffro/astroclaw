import * as cdk from 'aws-cdk-lib/core';
import { Construct } from 'constructs';
import * as dsql from 'aws-cdk-lib/aws-dsql';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as cr from 'aws-cdk-lib/custom-resources';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import { execSync } from 'child_process';
import * as path from 'path';

const projectRoot = path.join(__dirname, '..', '..', '..', '..');

export class InfraStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // CDK DSQL Doc: https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.aws_dsql.CfnCluster.html
    // CloudFormation DSQL Doc: https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-dsql-cluster.html
    const cluster = new dsql.CfnCluster(this, 'DsqlCluster', {
      deletionProtectionEnabled: false,
      tags: [{ key: 'Project', value: 'iclaw' }],
      // multiRegionProperties: not needed now, just single-region deployment
      // kmsEncryptionKey: using AWS-managed encryption (default)
      // policyDocument: no cross-account access needed
    });

    // Lambda CDK docs: https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.aws_lambda.Function.html
    // Lambda custom runtime docs: https://docs.aws.amazon.com/lambda/latest/dg/runtimes-custom.html
    const apiHandler = new lambda.Function(this, 'ApiHandler', {
      // Lambda runtimes docs: https://docs.aws.amazon.com/lambda/latest/dg/lambda-runtimes.html
      runtime: lambda.Runtime.PROVIDED_AL2023,
      handler: 'bootstrap',
      architecture: lambda.Architecture.ARM_64,
      code: lambda.Code.fromAsset(projectRoot, {
        bundling: {
          local: {
            // ILocalBundling Doc: https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.ILocalBundling.html
            // tryBundle method is called before attempting docker bundling to allow the bundler to be executed locally.
            // If the local bundler exists, and bundling was performed locally, return true. Otherwise, return false.
            tryBundle(outputDir: string): boolean {
              try {
                execSync(
                    `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o ${outputDir}/bootstrap ./deploy/aws/lambda/api`,
                    { cwd: projectRoot, stdio: 'inherit' },
                );
                return true;
              } catch {
                return false;
              }
            },
          },

          //DockerBuildOptions Doc: https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.DockerBuildOptions.html
          image: cdk.DockerImage.fromRegistry('golang:1.26'),
          command: [
            'bash', '-c',
            'cd /asset-input && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o /asset-output/bootstrap ./deploy/aws/lambda/api',
          ],
        },
      }),
      environment: {
        DSQL_ENDPOINT: cluster.attrEndpoint,
      },
      // TODO: API Gateway HTTP API has a 30-second max timeout. Agent loop (multiple
      // LLM calls + tool executions) can exceed this. Need async pattern, WebSocket,
      // or Lambda response streaming for long-running interactions.
      // https://docs.aws.amazon.com/apigateway/latest/developerguide/limits.html
      timeout: cdk.Duration.minutes(5),
      memorySize: 256,
    });

    const migrateHandler = new lambda.Function(this, 'MigrateHandler', {
      runtime: lambda.Runtime.PROVIDED_AL2023,
      handler: 'bootstrap',
      architecture: lambda.Architecture.ARM_64,
      code: lambda.Code.fromAsset(projectRoot, {
        bundling: {
          local: {
            tryBundle(outputDir: string): boolean {
              try {
                execSync(
                    `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o ${outputDir}/bootstrap ./deploy/aws/lambda/migrate`,
                    { cwd: projectRoot, stdio: 'inherit' },
                );
                // Bundle migration SQL files alongside the binary.
                execSync(`cp -r ${path.join(projectRoot, 'migrations')} ${outputDir}/migrations`);
                return true;
              } catch {
                return false;
              }
            },
          },
          image: cdk.DockerImage.fromRegistry('golang:1.26'),
          command: [
            'bash', '-c',
            'cd /asset-input && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o /asset-output/bootstrap ./deploy/aws/lambda/migrate && cp -r migrations /asset-output/migrations',
          ],
        },
      }),
      environment: {
        DSQL_ENDPOINT: cluster.attrEndpoint,
      },
      timeout: cdk.Duration.minutes(5),
      memorySize: 256,
    });

    // IAM permissions for DSQL access.
    // https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazonauroradsql.html

    // API Lambda: read/write data only, no schema changes.
    apiHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['dsql:DbConnect'],
      resources: [cluster.attrResourceArn],
    }));

    // Migrate Lambda: needs DDL access for CREATE TABLE, ALTER TABLE, etc.
    migrateHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['dsql:DbConnectAdmin'],
      resources: [cluster.attrResourceArn],
    }));

    // Run database migrations on every deployment via CloudFormation Custom Resource.
    // https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.custom_resources.Provider.html
    const migrateProvider = new cr.Provider(this, 'MigrateProvider', {
      onEventHandler: migrateHandler,
    });

    new cdk.CustomResource(this, 'RunMigrations', {
      serviceToken: migrateProvider.serviceToken,
      properties: {
        // Changing this value triggers the migrate Lambda on each deployment.
        version: Date.now().toString(),
      },
    });
  }
}
