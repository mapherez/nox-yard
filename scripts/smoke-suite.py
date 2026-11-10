#!/usr/bin/env python3
"""Check real Yard/Backend pairing using disposable data and published Auth.

Seeds only an isolated Backend API-key account, with central admission supplied
by real Auth. Does not contact Google, installed services or application data.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess
import time
import urllib.error
import urllib.request

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--image',required=True)
parser.add_argument('--backend-image',default='ghcr.io/mapherez/nox-backend:v1.2.0')
parser.add_argument('--gateway-image', help='Also validate routed menus and guarded execution using this local/public Gateway image')
parser.add_argument('--lifecycle', action='store_true', help='Also run actual managed product lifecycle; requires a Backend image with scoped installer deployment management')
parser.add_argument('--auth-image',default='ghcr.io/mapherez/nox-auth:v0.0.2')
args=parser.parse_args()
root=Path(__file__).resolve().parent.parent
name='nox-yard-suite-'+secrets.token_hex(6)
base=root/'.tmp'/name;base.mkdir(parents=True)
data=base/'backend';data.mkdir()
containers=[];volumes=[];images=[]
issuer='https://auth.example.test'
key='noxsync_'+secrets.token_urlsafe(32)


def docker(*parts):
    return subprocess.check_output(['docker',*map(str,parts)],text=True).strip()


def run(suffix,image,*parts):
    container=name+'-'+suffix;containers.append(container)
    docker('run','-d','--name',container,'--label','nox-yard.acceptance='+name,*parts,image)
    return container


def request(origin,path,body=None,method=None,headers=None):
    supplied={'Content-Type':'application/json','Authorization':'Bearer '+key,**(headers or {})}
    request=urllib.request.Request(origin+path,data=json.dumps(body).encode() if body is not None else None,method=method,headers=supplied)
    try:
        response=urllib.request.urlopen(request,timeout=30)
    except urllib.error.HTTPError as error:
        response=error
    with response:
        raw=response.read()
        value=json.loads(raw) if raw else None
        return response.status,value


def ready(origin,path):
    for _ in range(120):
        try:
            if request(origin,path)[0]==200:return
        except (OSError,ValueError):pass
        time.sleep(.25)
    raise AssertionError('Disposable participant did not become ready')


try:
    for image in [args.auth_image,args.backend_image]:
        if image.startswith('ghcr.io/'):
            docker('pull',image)
        else:
            docker('image','inspect',image)
    runtime,admin,auth_data,yard_data=[name+suffix for suffix in ['-runtime','-admin','-auth-data','-yard-data']]
    for volume in [runtime,admin,auth_data,yard_data]:
        docker('volume','create','--label','nox-yard.acceptance='+name,volume);volumes.append(volume)
    auth=run('auth',args.auth_image,'-p','127.0.0.1::8080','-v',auth_data+':/data','-v',runtime+':/run/nox-suite','-v',admin+':/run/nox-auth-admin','-e','NOX_AUTH_PUBLIC_URL='+issuer,'-e','NOX_AUTH_GOOGLE_CLIENT_ID=unused-suite-acceptance','-e','NOX_AUTH_GOOGLE_CLIENT_SECRET=unused-suite-acceptance','-e','NOX_AUTH_ALLOWED_EMAILS=owner@example.test')
    ready('http://'+docker('port',auth,'8080/tcp'),'/healthz')
    backend_options=['--user',f'{os.getuid()}:{os.getgid()}','--group-add','10001','-v',str(data.resolve())+':/data','-v',runtime+':/run/nox-suite','-e','NOX_BACKEND_PUBLIC_URL=http://backend.example.test','-e','NOX_AUTH_PUBLIC_URL='+issuer]
    docker('run','--rm',*backend_options,'-v',admin+':/run/nox-auth-admin',args.backend_image,'auth-setup')
    # The actual setup initialized and closed SQLite before this offline seed.
    stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    with sqlite3.connect(data/'nox-backend.db') as database:
        database.execute('INSERT INTO users(id,email,first_name,display_name,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)',('suite_acceptance_owner','owner@example.test','','Owner','ADMIN','ACTIVE',stamp,stamp))
        database.execute('INSERT INTO api_keys(user_id,key_prefix,key_value,key_hash,created_at) VALUES(?,?,?,?,?)',('suite_acceptance_owner','noxsync_',key,hashlib.sha256(key.encode()).hexdigest(),stamp))
    backend=run('backend',args.backend_image,*backend_options,'-p','127.0.0.1::8080')
    backend_origin='http://'+docker('port',backend,'8080/tcp');ready(backend_origin,'/v1/health')
    yard=run('yard',args.image,'-p','127.0.0.1::8080','-v',yard_data+':/data','-v',runtime+':/run/nox-suite','-v','/var/run/docker.sock:/var/run/docker.sock','-e','NOX_PUBLIC_URL=http://yard.example.test','-e','NOX_AUTH_PUBLIC_URL='+issuer,'-e','NOX_SUITE_LOCAL_VOLUME_NAME='+runtime,'-e','NOX_YARD_SUITE_SOCKET_DIR=/run/nox-suite')
    yard_origin='http://'+docker('port',yard,'8080/tcp');ready(yard_origin,'/healthz')
    status,declaration=request(yard_origin,'/nox/integration');assert status==200 and declaration['productId']=='nox-yard'
    for method in ['GET','POST']:
        status,_=request(yard_origin,'/mcp/suite',{} if method=='POST' else None,method,{'X-Forwarded-Proto':'https','X-Nox-Suite-Transport':'unix'});assert status==404,(method,status)
    status,saved=request(backend_origin,'/api/admin/connections/yard',{'origin':'http://yard.example.test'},'PUT',{'If-None-Match':'*'});assert status==200,(status,saved)
    status,verified=request(backend_origin,'/api/admin/connections/yard/verify',{'revision':saved['revision']},'POST');assert status==200,(status,verified)
    assert verified['productId']=='nox-yard'
    print('PASS: real Backend verifies the real Yard declaration through the local socket',flush=True)
    status,connected=request(backend_origin,'/api/admin/connections/yard/connect',{'attemptId':verified['attemptId']},'POST');assert status==200,(status,connected)
    assert connected['state']=='completed',(status,connected)
    status,state=request(backend_origin,'/api/admin/suite');assert status==200
    before=[p for p in state['state']['products'] if p['productId']=='nox-yard'];assert len(before)==1
    identity=before[0]['associationId']
    print('PASS: automatic pairing, ACK and real Yard registration; no manual protocol credentials',flush=True)
    docker('restart',yard)
    yard_origin='http://'+docker('port',yard,'8080/tcp')
    ready(yard_origin,'/healthz')
    for _ in range(120):
        status,state=request(backend_origin,'/api/admin/suite')
        after=[p for p in state['state']['products'] if p['productId']=='nox-yard']
        if len(after)==1 and after[0]['associationId']==identity:break
        time.sleep(.25)
    else:raise AssertionError('Yard restart changed registration identity')
    print('PASS: native HTTP remains available, forged network claims rejected, identity retained after restart',flush=True)
    if args.gateway_image:
        gateway=run('gateway',args.gateway_image,'-p','127.0.0.1::8080','--tmpfs','/data:uid=10002,gid=10001,mode=0700','-v',runtime+':/run/nox-suite')
        gateway_origin='http://'+docker('port',gateway,'8080/tcp');ready(gateway_origin,'/healthz')
        status,saved=request(backend_origin,'/api/admin/connections/gateway',{'origin':'http://gateway.example.test'},'PUT',{'If-None-Match':'*'});assert status==200,(status,saved)
        status,verified=request(backend_origin,'/api/admin/connections/gateway/verify',{'revision':saved['revision']},'POST');assert status==200,(status,verified)
        status,connected=request(backend_origin,'/api/admin/connections/gateway/connect',{'attemptId':verified['attemptId']},'POST');assert status==200 and connected['state']=='completed',(status,connected)
        ready(gateway_origin,'/readyz')
        def rpc(method,params):
            params={**params,'_meta':{'io.modelcontextprotocol/protocolVersion':'2026-07-28','io.modelcontextprotocol/clientCapabilities':{'extensions':{'nox/discovery':{'version':'1.0'}}}}}
            headers={'Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':method}
            if 'name' in params:headers['Mcp-Name']=params['name']
            status,value=request(gateway_origin,'/mcp',{'jsonrpc':'2.0','id':1,'method':method,'params':params},'POST',headers)
            assert status==200 and 'result' in value and 'error' not in value,(method,status,value)
            return value['result']
        standard=rpc('tools/list',{})
        yard_tools=[tool for tool in standard['tools'] if tool['name'].startswith('nox-yard__')]
        assert len(yard_tools)==38
        assert all('cli' not in tool.get('_meta',{}) for tool in yard_tools),'Menu paths leaked into conventional listing'
        catalog=rpc('nox/tools/catalog',{})
        refs=[{'name':tool['name'],'fingerprint':tool['fingerprint']} for tool in catalog['tools'] if tool['name'].startswith('nox-yard__')]
        definitions=rpc('nox/tools/definitions',{'catalog':catalog['catalog'],'tools':refs})['definitions']
        assert len(definitions)==38 and all(d['cli']['version']=='1.0' and d['tool']['_meta']['cli'] for d in definitions)
        remove=next(d for d in definitions if d['tool']['name']=='nox-yard__yard_container_remove')
        assert remove['cli']['preview']['tool']=='nox-yard__yard_container_remove_preview'
        assert remove['cli']['confirmation']['bindPointer']=='/confirm'
        status_result=rpc('tools/call',{'name':'nox-yard__yard_status','arguments':{}})
        assert not status_result.get('isError') and status_result['structuredContent']['service']=='nox-yard'
        # A uniquely labeled disposable target, never an existing host workload.
        target_name=name+'-menu-target';containers.append(target_name)
        target=docker('run','-d','--name',target_name,'--label','nox-yard.acceptance='+name,'alpine:3.23','sleep','300')
        assert docker('inspect','--format','{{ index .Config.Labels "nox-yard.acceptance" }}',target)==name
        preview=rpc('tools/call',{'name':remove['cli']['preview']['tool'],'arguments':{'id':target,'removeVolumes':False,'removeImages':False}})
        assert not preview.get('isError'),preview
        fingerprint=preview['structuredContent']['fingerprint']
        rejected=rpc('tools/call',{'name':remove['tool']['name'],'arguments':{'id':target,'fingerprint':fingerprint,'confirm':False}})
        assert rejected.get('isError') and 'structuredContent' not in rejected
        assert docker('inspect','--format','{{.Id}}',target)==target,'Unconfirmed removal changed the target'
        removed=rpc('tools/call',{'name':remove['tool']['name'],'arguments':{'id':target,'fingerprint':fingerprint,'confirm':True,'removeVolumes':False,'removeImages':False}})
        assert not removed.get('isError'),removed
        assert not docker('ps','-aq','--filter','id='+target),'Confirmed target was not removed'
        print('PASS: actual Gateway exposes 38 private Yard menu definitions, executes status, and enforces preview/confirmation before removing only the disposable target',flush=True)
    if args.lifecycle:
        docker('stop',yard)
        projects=base/'projects';projects.mkdir()
        host_projects=str(projects.resolve()).replace('\\','/')
        host_script=str((root/'scripts/fixtures/suite-product.py').resolve()).replace('\\','/')
        if host_projects[1:2]==':':
            host_projects='/run/desktop/mnt/host/'+host_projects[0].lower()+'/'+host_projects[3:]
            host_script='/run/desktop/mnt/host/'+host_script[0].lower()+'/'+host_script[3:]
        runner=name+':runner';images.append(runner)
        (base/'Dockerfile').write_text('FROM '+args.image+' AS tools\nFROM golang:1.26-alpine\nCOPY --from=tools /usr/bin/docker /usr/bin/docker\nCOPY --from=tools /usr/libexec/docker/cli-plugins/docker-compose /usr/libexec/docker/cli-plugins/docker-compose\n',encoding='utf-8')
        docker('build','-t',runner,base)
        environment={'NOX_HELPER_IMAGE':args.image,'NOX_SUITE_ACCEPTANCE_BASE':host_projects,
                     'NOX_SUITE_ACCEPTANCE_NAME':name,'NOX_SUITE_ACCEPTANCE_BACKEND_CONTAINER':backend,'NOX_SUITE_ACCEPTANCE_PRODUCT_IMAGE':'python:3.14-alpine',
                     'NOX_SUITE_ACCEPTANCE_PRODUCT_SCRIPT':host_script,'NOX_SUITE_ACCEPTANCE_ADMIN_KEY':key,
                     'NOX_SUITE_ACCEPTANCE_YARD_VERSION':declaration['productVersion'],
                     'NOX_SUITE_LOCAL_VOLUME_NAME':runtime,'NOX_YARD_SUITE_SOCKET_DIR':'/run/nox-suite'}
        docker('pull','python:3.14-alpine')
        command=['docker','run','--rm','--name',name+'-runner','--label','nox-yard.acceptance='+name,
                 '-v',str(root)+':/src:ro','-w','/src','-v',host_projects+':'+host_projects,
                 '-v',yard_data+':/data','-v',runtime+':/run/nox-suite','-v','/var/run/docker.sock:/var/run/docker.sock',
                 '-v','nox-yard-c2-go-cache:/go/pkg','-v','nox-yard-c2-build-cache:/root/.cache/go-build']
        containers.append(name+'-runner')
        for variable,value in environment.items():command+=['-e',variable+'='+value]
        command += [runner,'go','test','-v','-count=1','-timeout=8m','-run','^TestSuiteDeploymentDockerAcceptance$','./internal/managed']
        completed=subprocess.run(command,check=False)
        assert completed.returncode==0,'Real product lifecycle acceptance failed'

except Exception:
    for container in containers:
        subprocess.run(['docker','logs','--tail','15',container],check=False,stderr=subprocess.DEVNULL)
    raise
finally:
    # Product/helper containers created by the real workers carry this unique label.
    remaining=docker('ps','-aq','--filter','label=nox-yard.acceptance='+name).splitlines()
    for container in remaining:
        subprocess.run(['docker','rm','-f',container],check=False,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    for project in [name,name+'-reinstalled']:
        networks=docker('network','ls','-q','--filter','label=com.docker.compose.project='+project).splitlines()
        if networks:subprocess.run(['docker','network','rm',*networks],check=False,stdout=subprocess.DEVNULL)
    for container in reversed(containers):
        subprocess.run(['docker','rm','-f',container],check=False,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    for volume in reversed(volumes):
        subprocess.run(['docker','volume','rm',volume],check=True,stdout=subprocess.DEVNULL)
    for image in reversed(images):subprocess.run(['docker','image','rm',image],check=False,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    # The real worker writes root-owned files; reuse the portable fixture cleanup.
    from smoke_support import cleanup_fixture
    cleanup_fixture(base,root,name,args.image)
