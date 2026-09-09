"""Latch the application off after an account-wide AWS Budget alarm.

The SNS delivery sets the latch. A six-hour EventBridge check then re-applies
the shutdown while it remains set, chiefly because RDS automatically starts a
stopped database after seven days. Recovery is deliberately manual.
"""

from __future__ import annotations

import logging
import os

import boto3


log = logging.getLogger()
log.setLevel(logging.INFO)

ecs = boto3.client("ecs")
ec2 = boto3.client("ec2")
rds = boto3.client("rds")
ssm = boto3.client("ssm")

CLUSTER = os.environ["ECS_CLUSTER"]
ECS_SERVICES = tuple(filter(None, os.environ["ECS_SERVICES"].split(",")))
EC2_INSTANCE_IDS = tuple(filter(None, os.environ["EC2_INSTANCE_IDS"].split(",")))
RDS_INSTANCE_IDS = tuple(filter(None, os.environ["RDS_INSTANCE_IDS"].split(",")))
LATCH_PARAMETER = os.environ["LATCH_PARAMETER"]


def _is_budget_alarm(event: dict) -> bool:
    return any(record.get("EventSource") == "aws:sns" for record in event.get("Records", []))


def _latch_is_set() -> bool:
    try:
        value = ssm.get_parameter(Name=LATCH_PARAMETER)["Parameter"]["Value"]
    except ssm.exceptions.ParameterNotFound:
        return False
    return value.lower() == "true"


def _shutdown() -> None:
    errors: list[str] = []

    for service in ECS_SERVICES:
        try:
            ecs.update_service(cluster=CLUSTER, service=service, desiredCount=0)
            log.warning("scaled ECS service %s to zero", service)
        except Exception as exc:  # retry the SNS delivery if any target fails
            errors.append(f"ECS {service}: {exc}")

    if EC2_INSTANCE_IDS:
        try:
            ec2.stop_instances(InstanceIds=list(EC2_INSTANCE_IDS))
            log.warning("requested stop for EC2 instances %s", EC2_INSTANCE_IDS)
        except Exception as exc:
            errors.append(f"EC2: {exc}")

    for identifier in RDS_INSTANCE_IDS:
        try:
            status = rds.describe_db_instances(DBInstanceIdentifier=identifier)[
                "DBInstances"
            ][0]["DBInstanceStatus"]
            if status == "available":
                rds.stop_db_instance(DBInstanceIdentifier=identifier)
                log.warning("requested stop for RDS instance %s", identifier)
            else:
                log.info("RDS instance %s is %s; no stop needed now", identifier, status)
        except Exception as exc:
            errors.append(f"RDS {identifier}: {exc}")

    if errors:
        raise RuntimeError("; ".join(errors))


def handler(event: dict, _context: object) -> dict:
    if _is_budget_alarm(event):
        ssm.put_parameter(Name=LATCH_PARAMETER, Value="true", Type="String", Overwrite=True)
        log.warning("monthly budget crossed; shutdown latch set")
    elif not _latch_is_set():
        log.info("scheduled check found shutdown latch clear")
        return {"shutdown": False}

    _shutdown()
    return {"shutdown": True}
