import * as cdk from 'aws-cdk-lib/core';
import { Construct } from 'constructs';
import * as dsql from 'aws-cdk-lib/aws-dsql';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as cr from 'aws-cdk-lib/custom-resources';
import * as apigwv2 from 'aws-cdk-lib/aws-apigatewayv2';
import * as integrations from 'aws-cdk-lib/aws-apigatewayv2-integrations';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import { execSync } from 'child_process';
import * as crypto from 'crypto';
import * as path from 'path';

const projectRoot = path.join(__dirname, '..', '..', '..', '..');

export class InfraStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // LLM API key, passed in via: cdk deploy --parameters AnthropicApiKey=sk-ant-xxx
    const anthropicApiKey = new cdk.CfnParameter(this, 'AnthropicApiKey', {
      type: 'String',
      default: '',
      noEcho: true,
      description: 'Anthropic API key.',
    });

    // API authentication key. When GenerateApiKey=true, a random key is
    // generated at synth time, passed to migrate Lambda to seed into
    // credentials table, and printed in the stack output.
    //
    // IMPORTANT: always pass this parameter explicitly on every deploy.
    // CloudFormation retains the previous parameter value when omitted,
    // so if you deployed with true once and then omit the parameter,
    // it stays true and overwrites your API key with a new one.
    //
    // First deploy:      --parameters GenerateApiKey=true
    // Subsequent deploys: --parameters GenerateApiKey=false
    const generateApiKey = new cdk.CfnParameter(this, 'GenerateApiKey', {
      type: 'String',
      default: 'false',
      allowedValues: ['true', 'false'],
      description: 'ALWAYS pass explicitly. true = generate new key (overwrites existing). false = keep existing key.',
    });

    const generatedApiKey = crypto.randomUUID();

    // CDK CfnCondition docs: https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.CfnCondition.html
    const shouldGenerateApiKey = new cdk.CfnCondition(this, 'ShouldGenerateApiKey', {
      expression: cdk.Fn.conditionEquals(generateApiKey.valueAsString, 'true'),
    });

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
      timeout: cdk.Duration.seconds(30),
      memorySize: 256,
    });

    const wsApi = new apigwv2.WebSocketApi(this, 'WebSocketApi', {
      apiName: 'astroclaw-ws',
    });

    // WebSocket API for real-time event push (text streaming, tool status, etc.).
    // Clients connect with: wss://xxx.execute-api.region.amazonaws.com/prod?user_id=xxx&api_key=xxx
    // https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api.html
    const wsConnectHandler = new lambda.Function(this, 'WsConnectHandler', {
      runtime: lambda.Runtime.PROVIDED_AL2023,
      handler: 'bootstrap',
      architecture: lambda.Architecture.ARM_64,
      code: lambda.Code.fromAsset(projectRoot, {
        bundling: {
          local: {
            tryBundle(outputDir: string): boolean {
              try {
                execSync(
                    `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o ${outputDir}/bootstrap ./deploy/aws/lambda/wsconnect`,
                    { cwd: projectRoot, stdio: 'inherit' },
                );
                return true;
              } catch {
                return false;
              }
            },
          },
          image: cdk.DockerImage.fromRegistry('golang:1.26'),
          command: [
            'bash', '-c',
            'cd /asset-input && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o /asset-output/bootstrap ./deploy/aws/lambda/wsconnect',
          ],
        },
      }),
      environment: {
        DSQL_ENDPOINT: cluster.attrEndpoint,
      },
      timeout: cdk.Duration.seconds(10),
      memorySize: 128,
    });

    const wsDisconnectHandler = new lambda.Function(this, 'WsDisconnectHandler', {
      runtime: lambda.Runtime.PROVIDED_AL2023,
      handler: 'bootstrap',
      architecture: lambda.Architecture.ARM_64,
      code: lambda.Code.fromAsset(projectRoot, {
        bundling: {
          local: {
            tryBundle(outputDir: string): boolean {
              try {
                execSync(
                    `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o ${outputDir}/bootstrap ./deploy/aws/lambda/wsdisconnect`,
                    { cwd: projectRoot, stdio: 'inherit' },
                );
                return true;
              } catch {
                return false;
              }
            },
          },
          image: cdk.DockerImage.fromRegistry('golang:1.26'),
          command: [
            'bash', '-c',
            'cd /asset-input && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o /asset-output/bootstrap ./deploy/aws/lambda/wsdisconnect',
          ],
        },
      }),
      environment: {
        DSQL_ENDPOINT: cluster.attrEndpoint,
      },
      timeout: cdk.Duration.seconds(10),
      memorySize: 128,
    });

    // DSQL access for WebSocket Lambdas.
    wsConnectHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['dsql:DbConnectAdmin'],
      resources: [cluster.attrResourceArn],
    }));
    wsDisconnectHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['dsql:DbConnectAdmin'],
      resources: [cluster.attrResourceArn],
    }));

    wsApi.addRoute('$connect', {
      integration: new integrations.WebSocketLambdaIntegration('WsConnectIntegration', wsConnectHandler),
    });

    wsApi.addRoute('$disconnect', {
      integration: new integrations.WebSocketLambdaIntegration('WsDisconnectIntegration', wsDisconnectHandler),
    });

    // API Gateway Stage docs: https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-deployment.html
    const wsStage = new apigwv2.WebSocketStage(this, 'WebSocketStage', {
      webSocketApi: wsApi,
      stageName: 'prod',
      autoDeploy: true,
    });

    // Reply Lambda: handles /reply requests with agent loop + LLM calls.
    // Uses Function URL instead of API Gateway to avoid the 30-second timeout.
    // https://docs.aws.amazon.com/lambda/latest/dg/urls-invocation.html
    const replyHandler = new lambda.Function(this, 'ReplyHandler', {
      runtime: lambda.Runtime.PROVIDED_AL2023,
      handler: 'bootstrap',
      architecture: lambda.Architecture.ARM_64,
      code: lambda.Code.fromAsset(projectRoot, {
        bundling: {
          local: {
            tryBundle(outputDir: string): boolean {
              try {
                execSync(
                    `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o ${outputDir}/bootstrap ./deploy/aws/lambda/reply`,
                    { cwd: projectRoot, stdio: 'inherit' },
                );
                return true;
              } catch {
                return false;
              }
            },
          },
          image: cdk.DockerImage.fromRegistry('golang:1.26'),
          command: [
            'bash', '-c',
            'cd /asset-input && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o /asset-output/bootstrap ./deploy/aws/lambda/reply',
          ],
        },
      }),
      environment: {
        DSQL_ENDPOINT: cluster.attrEndpoint,
        WS_ENDPOINT: wsStage.callbackUrl,
      },
      timeout: cdk.Duration.minutes(15),
      memorySize: 512,
    });

    // Allow Reply Lambda to push events to WebSocket clients via PostToConnection.
    // PostToConnection is part of the API Gateway Management API (@connections),
    // which requires execute-api:ManageConnections permission on the WebSocket API.
    // https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-how-to-call-websocket-api-connections.html
    replyHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['execute-api:ManageConnections'],
      resources: [
        `arn:aws:execute-api:${this.region}:${this.account}:${wsApi.apiId}/*`,
      ],
    }));

    // Function URL for Reply Lambda (no API Gateway timeout limit).
    const replyUrl = replyHandler.addFunctionUrl({
      authType: lambda.FunctionUrlAuthType.NONE,
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
        ANTHROPIC_API_KEY: anthropicApiKey.valueAsString,
        GENERATED_API_KEY: cdk.Fn.conditionIf(
          'ShouldGenerateApiKey', generatedApiKey, '',
        ).toString(),
      },
      timeout: cdk.Duration.minutes(5),
      memorySize: 256,
    });

    // IAM permissions for DSQL access.
    // https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazonauroradsql.html

    // API Lambda: using DbConnectAdmin for now. Switch to DbConnect with a
    // restricted database role when fine-grained table access control is needed.
    apiHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['dsql:DbConnectAdmin'],
      resources: [cluster.attrResourceArn],
    }));

    // Reply Lambda: same as API Lambda for now.
    replyHandler.addToRolePolicy(new iam.PolicyStatement({
      actions: ['dsql:DbConnectAdmin'],
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

    // HTTP API Gateway routes all requests to the API Lambda.
    // The Lambda's handler() function does the actual path/method routing.
    // https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.aws_apigatewayv2.HttpApi.html
    const api = new apigwv2.HttpApi(this, 'HttpApi', {
      apiName: 'iclaw-api',
    });

    const lambdaIntegration = new integrations.HttpLambdaIntegration(
      'LambdaIntegration', apiHandler,
    );

    // /{proxy+} is a catch-all route that forwards every path to the Lambda.
    api.addRoutes({
      path: '/{proxy+}',
      methods: [apigwv2.HttpMethod.ANY],
      integration: lambdaIntegration,
    });

    new cdk.CfnOutput(this, 'ApiUrl', {
      value: api.url!,
      description: 'API Gateway endpoint (session CRUD)',
    });

    new cdk.CfnOutput(this, 'ReplyUrl', {
      value: replyUrl.url,
      description: 'Function URL endpoint (reply/agent)',
    });

    new cdk.CfnOutput(this, 'WebSocketUrl', {
      value: wsStage.url,
      description: 'WebSocket endpoint for real-time events',
    });

    new cdk.CfnOutput(this, 'GeneratedApiKey', {
      value: generatedApiKey,
      description: 'SAVE THIS NOW. Use it in the x-api-key header for all API requests.',
      condition: shouldGenerateApiKey
    });
  }
}
