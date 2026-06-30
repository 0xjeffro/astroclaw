import * as cdk from 'aws-cdk-lib/core';
import { Construct } from 'constructs';
import * as dsql from 'aws-cdk-lib/aws-dsql';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as cr from 'aws-cdk-lib/custom-resources';
import * as apigwv2 from 'aws-cdk-lib/aws-apigatewayv2';
import * as integrations from 'aws-cdk-lib/aws-apigatewayv2-integrations';
import * as kms from 'aws-cdk-lib/aws-kms';
import * as lambda from 'aws-cdk-lib/aws-lambda';
import * as s3 from 'aws-cdk-lib/aws-s3';
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

    // Initial admin password. 24 bytes of crypto/rand encoded as base64url
    // gives a ~32-char URL-safe string with 192 bits of entropy.
    //
    // First deploy: --parameters GenerateAdminPassword=true
    // Subsequent deploys: --parameters GenerateAdminPassword=false
    const generateAdminPassword = new cdk.CfnParameter(this, 'GenerateAdminPassword', {
      type: 'String',
      default: 'false',
      allowedValues: ['true', 'false'],
      description: 'ALWAYS pass explicitly. true = generate a new admin password (overwrites existing). false = keep existing.',
    });

    const generatedAdminPassword = crypto.randomBytes(24).toString('base64url');

    const shouldGenerateAdminPassword = new cdk.CfnCondition(this, 'ShouldGenerateAdminPassword', {
      expression: cdk.Fn.conditionEquals(generateAdminPassword.valueAsString, 'true'),
    });

    // CDK DSQL Doc: https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.aws_dsql.CfnCluster.html
    // CloudFormation DSQL Doc: https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-dsql-cluster.html
    const cluster = new dsql.CfnCluster(this, 'DsqlCluster', {
      deletionProtectionEnabled: false,
      tags: [{ key: 'Project', value: 'astroclaw' }],
      // multiRegionProperties: not needed now, just single-region deployment
      // kmsEncryptionKey: using AWS-managed encryption (default)
      // policyDocument: no cross-account access needed
    });

    // S3 bucket for skill storage (ZIP archives).
    // Each skill is stored as skills/{author}/{name}.zip.
    // The @local/ namespace is reserved for user-created skills that
    // haven't been published to a registry yet (e.g. skills/@local/my-skill.zip).
    // https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.aws_s3.Bucket.html
    // removalPolicy is DESTROY so cdk destroy doesn't leave orphan buckets
    // that block redeployment. Data safety is handled at the script layer:
    // scripts/destroy.sh should back up bucket contents before destroying.
    // TODO: implement backup in scripts/destroy.sh
    const skillsBucket = new s3.Bucket(this, 'SkillsBucket', {
      removalPolicy: cdk.RemovalPolicy.DESTROY,
      autoDeleteObjects: true,
      blockPublicAccess: s3.BlockPublicAccess.BLOCK_ALL,
      encryption: s3.BucketEncryption.S3_MANAGED,
    });

    // KMS Customer Managed Key for envelope encryption of credentials.
    // Used by the passwords app: per-scope(user/workspace/system scope) data keys are encrypted with
    // this CMK and stored in DSQL; Lambdas decrypt the data key once at
    // cold start, cache it, then perform local AES-GCM encrypt/decrypt
    // on individual credentials. Yearly auto-rotation keeps a fresh key
    // version without invalidating existing ciphertexts.
    // https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html
    // In production we RETAIN the key so accidental stack deletion doesn't
    // make every encrypted credential permanently unrecoverable. Toggle via
    //   cdk deploy -c isProd=true
    const isProd = this.node.tryGetContext('isProd') === true;

    // Ephemeral CI stacks (per-PR) use the minimum 7-day KMS pending
    // window so cleanup is quick; long-lived stacks keep the safer
    // 30-day default. KMS does not charge for keys in PendingDeletion,
    // so this is just to keep the console clean.
    const isEphemeral = process.env.EPHEMERAL_STACK === 'true';
    const passwordsAlias = isEphemeral
        // The alias is suffixed with the stack name for ephemeral stacks so
        // parallel PR deploys do not collide on the region-unique alias name.
      ? `astroclaw-passwords-${process.env.STACK_NAME}`
      : 'astroclaw-passwords';
    const passwordsKey = new kms.Key(this, 'PasswordsKey', {
      alias: passwordsAlias,
      enableKeyRotation: true,
      description: 'Encrypts workspace/user/system scoped data keys for the passwords app',
      removalPolicy: isProd ? cdk.RemovalPolicy.RETAIN : cdk.RemovalPolicy.DESTROY,
      pendingWindow: cdk.Duration.days(isEphemeral ? 7 : 30),
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
        KMS_URL: `awskms://${passwordsKey.keyId}?region=${this.region}`,
      },
      timeout: cdk.Duration.seconds(30),
      memorySize: 256,
    });

    // API Lambda upserts workspace and user credentials.
    // Both Encrypt and Decrypt permission is required here because
    // 1. When we set credentials, we need it to "decrypt" the data key and use this decrypted data key to encrypt the credential.
    // 2. When we create a new user or workspace by lambda api, we need use this key to encrypt the data key of this user/ workspace.
    passwordsKey.grantEncryptDecrypt(apiHandler);

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
        SKILLS_BUCKET: skillsBucket.bucketName,
        KMS_URL: `awskms://${passwordsKey.keyId}?region=${this.region}`,
      },
      timeout: cdk.Duration.minutes(15),
      memorySize: 512,
    });

    // Grant Reply Lambda read access to the skills bucket.
    skillsBucket.grantRead(replyHandler);

    // Reply Lambda decrypts cached data keys at cold start;
    // We adhere to least-privilege principle here.
    passwordsKey.grantDecrypt(replyHandler);

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
                // Bundle migration SQL files and skills alongside the binary.
                execSync(`cp -r ${path.join(projectRoot, 'migrations')} ${outputDir}/migrations`);
                execSync(`cp -r ${path.join(projectRoot, 'skills')} ${outputDir}/skills`);
                return true;
              } catch {
                return false;
              }
            },
          },
          image: cdk.DockerImage.fromRegistry('golang:1.26'),
          command: [
            'bash', '-c',
            'cd /asset-input && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o /asset-output/bootstrap ./deploy/aws/lambda/migrate && cp -r migrations /asset-output/migrations && cp -r skills /asset-output/skills',
          ],
        },
      }),
      environment: {
        DSQL_ENDPOINT: cluster.attrEndpoint,
        ANTHROPIC_API_KEY: anthropicApiKey.valueAsString,
        GENERATED_ADMIN_PASSWORD: cdk.Fn.conditionIf(
          'ShouldGenerateAdminPassword', generatedAdminPassword, '',
        ).toString(),
        SKILLS_BUCKET: skillsBucket.bucketName,
        KMS_URL: `awskms://${passwordsKey.keyId}?region=${this.region}`,
      },
      timeout: cdk.Duration.minutes(5),
      memorySize: 256,
    });

    // Migrate Lambda needs S3 write access to seed default skills on first deploy.
    skillsBucket.grantReadWrite(migrateHandler);

    // Migrate Lambda generates the per-scope data keys on first deploy and
    // wraps them with the CMK before writing to DSQL.
    passwordsKey.grantEncryptDecrypt(migrateHandler);

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
      apiName: 'astroclaw-api',
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

    new cdk.CfnOutput(this, 'SkillsBucketName', {
      value: skillsBucket.bucketName,
      description: 'S3 bucket for skill storage',
    });

    new cdk.CfnOutput(this, 'GeneratedAdminPassword', {
      value: generatedAdminPassword,
      description: 'SAVE THIS NOW. Initial admin password for the seeded admin user; only shown on this deploy.',
      condition: shouldGenerateAdminPassword
    });
  }
}
