#!/usr/bin/env python3
"""Real isolated Yard replacement and failed replacement/SQLite rollback.

Only the fixture binary's restore-tag constant is remapped to a private local
image tag. Registry lookup/pull is outside this worker-protocol acceptance.
The shipped binary has no configuration override or production test hook.
"""
import argparse
import json
import pathlib
import re
import secrets
import shutil
import subprocess
import time

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--image',required=True)
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
name='self-update-smoke-'+secrets.token_hex(6)
base=root/'.tmp'/name
base.mkdir(parents=True,mode=0o700)
(base/'state').mkdir()
images=[]
reference=name+':latest'

def docker(*parts,stream=False):
 result=subprocess.run(['docker',*parts],text=True,capture_output=not stream)
 if result.returncode: raise RuntimeError('Self-update fixture Docker command failed: '+' '.join(parts[:2])+'\n'+(result.stderr or ''))
 return (result.stdout or '').strip()

try:
 source=base/'source';source.mkdir()
 for f in ('go.mod','go.sum'):shutil.copy2(root/f,source/f)
 for d in ('cmd','internal'):shutil.copytree(root/d,source/d)
 registry=source/'internal/selfupdate/registry.go'
 text=registry.read_text(encoding='utf-8');old='const imageReference = "ghcr.io/mapherez/nox-yard:latest"'
 assert text.count(old)==1
 registry.write_text(text.replace(old,'const imageReference = '+json.dumps(reference)),encoding='utf-8',newline='')
 (source/'bad').write_text("""#!/bin/sh
exec python3 - <<'PYTHON'
import sqlite3,pathlib
db=sqlite3.connect('/data/nox-yard.sqlite')
db.execute("UPDATE administrators SET username='broken'")
db.execute('CREATE TABLE fixture_bad_migration(value TEXT)')
db.commit()
db.close()
pathlib.Path('/data/.fixture-bad-ran').write_text('migration committed')
raise SystemExit(1)
PYTHON
""",encoding='utf-8',newline='')
 dockerfile=("FROM golang:1.26-alpine AS builder\nWORKDIR /src\nCOPY go.mod go.sum ./\nRUN go mod download\nCOPY cmd ./cmd\nCOPY internal ./internal\nRUN CGO_ENABLED=0 go build -o /out/nox-yard ./cmd/nox-yard\n"+
  "FROM "+args.image+" AS fixture\nCOPY --from=builder /out/nox-yard /usr/local/bin/nox-yard\nLABEL nox-yard.acceptance="+name+"\n"+
  "FROM fixture AS good\nLABEL fixture.variant=good\n"+
  "FROM fixture AS bad\nRUN apk add --no-cache python3\nCOPY bad /usr/local/bin/nox-yard\nRUN chmod +x /usr/local/bin/nox-yard\nLABEL fixture.variant=bad\n")
 (source/'Dockerfile').write_text(dockerfile,encoding='utf-8',newline='')
 for target in ('fixture','good','bad','builder'):
  tag=name+':'+target;images.append(tag)
  docker('build','--target',target,'-t',tag,str(source))
 docker('tag',name+':fixture',reference);images.append(reference)
 host_base=str(base.resolve()).replace('\\','/')
 if host_base[1:2]==':':host_base='/run/desktop/mnt/host/'+host_base[0].lower()+'/'+host_base[3:]
 yard=docker('create','--name',name+'-yard','--label','nox-yard.acceptance='+name,
  '--label','com.docker.compose.project='+name,'--label','com.docker.compose.service=nox-yard',
  '--mount','type=bind,source='+host_base+'/state,target=/data',
  '--mount','type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock',
  '--health-cmd','wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1',
  '--health-interval','1s','--health-timeout','1s','--health-retries','1',name+':fixture')
 docker('start',yard)
 deadline=time.monotonic()+60
 while json.loads(docker('inspect',yard))[0]['State']['Health']['Status']!='healthy':
  assert time.monotonic()<deadline,'Fixture did not become healthy'
  time.sleep(.2)
 docker('run','--rm','--label','nox-yard.acceptance='+name,
  '--mount','type=bind,source='+str(base/'state')+',target=/data',
  '--mount','type=bind,source='+str(base)+',target=/fixture',
  '--mount','type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock',
  '--mount','type=volume,source=nox-yard-c2-go-cache,target=/go/pkg',
  '--mount','type=volume,source=nox-yard-c2-build-cache,target=/root/.cache/go-build',
  '--env','NOX_SELF_FIXTURE_REFERENCE='+reference,'--env','NOX_SELF_FIXTURE_NAME='+name,
  '--env','NOX_SELF_FIXTURE_CONTAINER='+yard,'--env','NOX_SELF_FIXTURE_GOOD='+name+':good',
  '--env','NOX_SELF_FIXTURE_BAD='+name+':bad',name+':builder',
  'go','test','-v','-count=1','-timeout=4m','-run','^TestSelfUpdateDockerAcceptance$','./internal/selfupdate',stream=True)
finally:
 ledger=base/'worker-ids'
 if ledger.exists():
  for value in ledger.read_text().splitlines():
   if re.fullmatch(r'[0-9a-f]{64}|nox-yard-update-[0-9a-f]{24}',value):subprocess.run(['docker','rm','-f',value],capture_output=True)
 ids=docker('ps','-aq','--filter','label=nox-yard.acceptance='+name).splitlines()
 if ids:docker('rm','-f',*ids)
 for image in reversed(images):subprocess.run(['docker','image','rm',image],capture_output=True)
 assert base.resolve().is_relative_to((root/'.tmp').resolve()) and base.name==name
 shutil.rmtree(base)
