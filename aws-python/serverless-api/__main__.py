"""
AWS Serverless API with Pulumi (Python)
Production-grade serverless REST API using Lambda, API Gateway, and DynamoDB
"""

import json
import pulumi
import pulumi_aws as aws
import pulumi_aws_apigateway as apigateway

# Configuration
config = pulumi.Config()
environment = pulumi.get_stack()
app_name = "serverless-api"

# Tags
tags = {
    "Environment": environment,
    "ManagedBy": "Pulumi",
    "Application": app_name
}

# DynamoDB Table
table = aws.dynamodb.Table(
    f"{app_name}-table",
    name=f"{app_name}-{environment}-items",
    attributes=[
        aws.dynamodb.TableAttributeArgs(
            name="id",
            type="S",
        ),
    ],
    hash_key="id",
    billing_mode="PAY_PER_REQUEST",  # On-demand pricing
    point_in_time_recovery=aws.dynamodb.TablePointInTimeRecoveryArgs(
        enabled=True,
    ),
    server_side_encryption=aws.dynamodb.TableServerSideEncryptionArgs(
        enabled=True,
    ),
    tags=tags,
)

# IAM Role for Lambda
lambda_role = aws.iam.Role(
    f"{app_name}-lambda-role",
    assume_role_policy=json.dumps({
        "Version": "2012-10-17",
        "Statement": [{
            "Effect": "Allow",
            "Principal": {"Service": "lambda.amazonaws.com"},
            "Action": "sts:AssumeRole"
        }]
    }),
    tags=tags,
)

# Attach basic Lambda execution policy
aws.iam.RolePolicyAttachment(
    f"{app_name}-lambda-basic",
    role=lambda_role.name,
    policy_arn="arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionPolicy",
)

# DynamoDB access policy for Lambda
dynamodb_policy = aws.iam.RolePolicy(
    f"{app_name}-dynamodb-policy",
    role=lambda_role.id,
    policy=pulumi.Output.all(table.arn).apply(
        lambda args: json.dumps({
            "Version": "2012-10-17",
            "Statement": [{
                "Effect": "Allow",
                "Action": [
                    "dynamodb:PutItem",
                    "dynamodb:GetItem",
                    "dynamodb:UpdateItem",
                    "dynamodb:DeleteItem",
                    "dynamodb:Scan",
                    "dynamodb:Query"
                ],
                "Resource": args[0]
            }]
        })
    ),
)

# Lambda Function Code
lambda_code = """
import json
import boto3
import os
from decimal import Decimal

dynamodb = boto3.resource('dynamodb')
table = dynamodb.Table(os.environ['TABLE_NAME'])

class DecimalEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, Decimal):
            return float(obj)
        return super(DecimalEncoder, self).default(obj)

def handler(event, context):
    http_method = event['requestContext']['http']['method']
    path = event['requestContext']['http']['path']
    
    try:
        if http_method == 'GET' and path == '/items':
            response = table.scan()
            return {
                'statusCode': 200,
                'headers': {'Content-Type': 'application/json'},
                'body': json.dumps(response['Items'], cls=DecimalEncoder)
            }
        
        elif http_method == 'POST' and path == '/items':
            body = json.loads(event['body'])
            table.put_item(Item=body)
            return {
                'statusCode': 201,
                'headers': {'Content-Type': 'application/json'},
                'body': json.dumps({'message': 'Item created'})
            }
        
        elif http_method == 'GET' and path.startswith('/items/'):
            item_id = path.split('/')[-1]
            response = table.get_item(Key={'id': item_id})
            if 'Item' in response:
                return {
                    'statusCode': 200,
                    'headers': {'Content-Type': 'application/json'},
                    'body': json.dumps(response['Item'], cls=DecimalEncoder)
                }
            return {
                'statusCode': 404,
                'body': json.dumps({'error': 'Item not found'})
            }
        
        return {
            'statusCode': 404,
            'body': json.dumps({'error': 'Not found'})
        }
    
    except Exception as e:
        return {
            'statusCode': 500,
            'body': json.dumps({'error': str(e)})
        }
"""

# Lambda Function
lambda_func = aws.lambda_.Function(
    f"{app_name}-handler",
    name=f"{app_name}-{environment}-handler",
    role=lambda_role.arn,
    runtime="python3.11",
    handler="index.handler",
    code=pulumi.AssetArchive({
        "index.py": pulumi.StringAsset(lambda_code)
    }),
    environment=aws.lambda_.FunctionEnvironmentArgs(
        variables={
            "TABLE_NAME": table.name,
        },
    ),
    timeout=30,
    memory_size=256,
    tags=tags,
)

# CloudWatch Log Group with retention
log_group = aws.cloudwatch.LogGroup(
    f"{app_name}-logs",
    name=pulumi.Output.concat("/aws/lambda/", lambda_func.name),
    retention_in_days=7,
    tags=tags,
)

# API Gateway v2 (HTTP API)
api = apigateway.RestAPI(
    f"{app_name}-api",
    routes=[
        apigateway.RouteArgs(
            path="/items",
            method=apigateway.Method.GET,
            event_handler=lambda_func,
        ),
        apigateway.RouteArgs(
            path="/items",
            method=apigateway.Method.POST,
            event_handler=lambda_func,
        ),
        apigateway.RouteArgs(
            path="/items/{id}",
            method=apigateway.Method.GET,
            event_handler=lambda_func,
        ),
    ],
    stage_name=environment,
    tags=tags,
)

# Outputs
pulumi.export("api_url", api.url)
pulumi.export("table_name", table.name)
pulumi.export("lambda_function_name", lambda_func.name)
pulumi.export("region", aws.get_region().name)
