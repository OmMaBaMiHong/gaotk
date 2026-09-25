#!/usr/bin/env python3
"""Local-only membership fixture: real gateway/database, simulated model upstream."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / '.local-membership'
BASE = 'http://127.0.0.1:18441'
FREE = 'qwen/qwen3.8-27b:free'
MODELS = {
    FREE: ('openai', 0, 0, 0),
    'deepseek-flash': ('deepseek', 0.22 * 7.14, 0.66 * 7.14, 0.007 * 7.14),
    'glm-5.3-flash': ('openai', 0.8, 2.8, 0.23),
    'glm-5.3': ('openai', 8, 28, 2),
    'hy4-preview': ('openai', 6, 18, 0.3),
}
PASSWORD = 'Membership-local-2026!'
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def request(path, payload=None, token=None, method=None, expected=200):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(BASE + path, data=None if payload is None else json.dumps(payload).encode(), headers=headers, method=method or ('POST' if payload is not None else 'GET'))
    try:
        with OPENER.open(req, timeout=45) as res:
            status, data = res.status, json.load(res)
    except urllib.error.HTTPError as err:
        status, data = err.code, json.loads(err.read())
    allowed = expected if isinstance(expected, tuple) else (expected,)
    if status not in allowed:
        raise AssertionError(f'{path}: HTTP {status}: {json.dumps(data,ensure_ascii=False)[:500]}')
    return data.get('data', data) if path.startswith('/api/') else data

def sql(statement):
    value = subprocess.check_output(['docker','exec','-i','skoob-membership-pricing-pg','psql','-U','sub2api','-d','sub2api','-X','-t','-A','-v','ON_ERROR_STOP=1'],input=statement,text=True)
    return value.strip()

def login(email):
    return request('/api/v1/auth/login', {'email':email,'password':PASSWORD})['access_token']

def save(obj):
    path=STATE/'fixture.json'
    path.write_text(json.dumps(obj,ensure_ascii=False,indent=2))
    path.chmod(0o600)

def seed():
    if (STATE/'fixture.json').exists():
        print('Existing fixture preserved; run check.'); return
    admin=login('admin@membership.local')
    result={'base':BASE,'tiers':[],'simulation':True}
    selected=[('启笔',30,0,[FREE]),('成章',69,40,[FREE,'deepseek-flash','glm-5.3-flash']),('著作',99,70,list(MODELS))]
    for name,price,quota,models in selected:
        group=request('/api/v1/admin/groups',{'name':'本地模拟·焚诀·'+name,'description':'人民币订阅配置验证；模拟上游，未上线','platform':'composite','rate_multiplier':0.5,'subscription_type':'subscription','monthly_limit_usd':quota or None,'model_allowlist':{'enabled':True,'models':models}},admin,expected=(200,201))
        gid=group['id']
        for model in models:
            request(f'/api/v1/admin/groups/{gid}/composite-routes',{'public_model':model,'target_platform':MODELS[model][0],'upstream_model':model,'match_type':'exact','endpoint':'any','enabled':True,'priority':0},admin,expected=(200,201))
        plan=request('/api/v1/admin/payment/plans',{'group_id':gid,'name':'焚诀·'+name+'·月卡（本地模拟）','description':'人民币；模拟验证，未发布','price':price,'currency':'CNY','validity_days':30,'validity_unit':'day','features':json.dumps(['精选免费模型'] if not quota else [f'每月 {quota} 元共享额度']+models[1:],ensure_ascii=False),'product_name':'skoob','for_sale':False,'sort_order':len(result['tiers'])},admin,expected=(200,201))
        email=f'tier{gid}@membership.local'
        user=request('/api/v1/admin/users',{'email':email,'password':PASSWORD,'username':name+'本地测试','balance':0,'concurrency':3,'allowed_groups':[gid],'restrict_public_groups':True},admin,expected=(200,201))
        sub=request('/api/v1/admin/subscriptions/assign',{'user_id':user['id'],'group_id':gid,'validity_days':30,'notes':'本地零余额订阅验证'},admin,expected=(200,201))
        key=request('/api/v1/keys',{'name':'本地模拟-'+name,'group_id':gid},login(email),expected=(200,201))
        result['tiers'].append({'name':name,'group_id':gid,'plan_id':plan['id'],'user_id':user['id'],'subscription_id':sub['id'],'email':email,'key':key['key'],'models':models,'quota':quota})
    gids=[t['group_id'] for t in result['tiers']]
    for platform in ['openai','deepseek']:
        mapping={m:m for m,details in MODELS.items() if details[0]==platform}
        request('/api/v1/admin/accounts',{'name':'模拟上游-'+platform,'platform':platform,'type':'apikey','credentials':{'api_key':'local-fixture-key','base_url':'http://127.0.0.1:18442','model_mapping':mapping},'extra':{'openai_responses_supported':False,'upstream_billing_probe_enabled':False},'concurrency':5,'priority':1,'rate_multiplier':1,'group_ids':gids,'confirm_mixed_channel_risk':True},admin,expected=(200,201))
    def prices(scale, stats=False):
        return [{'platform':'composite' if stats else p,'models':[m],'billing_mode':'token','input_price':ci*scale/1e6,'output_price':co*scale/1e6,'cache_read_price':cc*scale/1e6} for m,(p,ci,co,cc) in MODELS.items()]
    channel=request('/api/v1/admin/channels',{'name':'本地人民币模型计价','description':'单价与成本均 CNY/token，基价为成本3倍、分组0.5倍；不使用线上密钥','group_ids':gids,'billing_model_source':'requested','restrict_models':True,'model_pricing':prices(3),'account_stats_pricing_rules':[{'name':'本地人民币成本','group_ids':gids,'account_ids':[],'pricing':prices(1, stats=True)}]},admin,expected=(200,201))
    result['channel_id']=channel['id'];save(result)
    print(json.dumps({'groups':gids,'channel_id':channel['id'],'plans':[t['plan_id'] for t in result['tiers']],'simulation':True},ensure_ascii=False))

def chat(key,model,expected=200):
    return request('/v1/chat/completions',{'model':model,'messages':[{'role':'user','content':'这是本地会员计费验证，请返回 OK。'}],'stream':False,'max_tokens':128},key,expected=expected)

def check():
    fixture=json.loads((STATE/'fixture.json').read_text()); checks=[]
    first_log_id=int(sql('SELECT COALESCE(max(id),0) FROM usage_logs;'))
    initial_usage=json.loads(sql("SELECT COALESCE(json_object_agg(group_id,monthly_usage_usd),'{}') FROM user_subscriptions;"))
    for tier in fixture['tiers']:
        listed=request('/v1/models',token=tier['key'])['data']
        ids=sorted(x['id'] for x in listed)
        assert ids==sorted(tier['models']),(tier['name'],ids)
        checks.append(tier['name']+'模型列表准确，未暴露旧型号')
        available=request('/api/v1/groups/available',token=login(tier['email']))
        assert any(x['id']==tier['group_id'] for x in available),available
        for model in tier['models']:
            chat(tier['key'],model)
        chat(tier['key'],'qwen3.5-flash',expected=(400,403,404))
        if tier['name']=='成章':
            chat(tier['key'],'glm-5.3',expected=(400,403,404))
            chat(tier['key'],'gpt-5.6',expected=(400,403,404))
        checks.append(tier['name']+'零现金余额可调用，越权模型被拒绝')
    time.sleep(2)
    report=json.loads(sql("SELECT json_agg(t) FROM (SELECT u.id,u.balance,s.monthly_usage_usd,s.group_id FROM users u JOIN user_subscriptions s ON s.user_id=u.id WHERE u.email LIKE 'tier%@membership.local' ORDER BY u.id) t;"))
    for row in report: assert float(row['balance'])==0,row
    logs=json.loads(sql(f"SELECT json_agg(t) FROM (SELECT group_id,model,actual_cost,account_stats_cost FROM usage_logs WHERE id>{first_log_id} ORDER BY id) t;"))
    for row in logs:
        p,ci,co,cc=MODELS[row['model']]
        want=(1000*ci+100*co)/1e6
        assert abs(float(row['actual_cost'])-want*1.5)<1e-7,row
        if want:
            assert abs(float(row['account_stats_cost'])-want)<1e-7,row
            assert 1-float(row['account_stats_cost'])/float(row['actual_cost'])>0.30,row
        else: assert float(row['actual_cost'])==0,row
    for row in report:
        cost=sum(float(x['actual_cost']) for x in logs if x['group_id']==row['group_id'])
        assert abs(float(row['monthly_usage_usd'])-float(initial_usage[str(row['group_id'])])-cost)<1e-7,(row,cost)
    checks.append('模型费用与人民币成本匹配；订阅扣额等于各模型费用之和，免费模型不扣额')
    target=fixture['tiers'][1]
    # Isolated test database only. Restore both values after fault injection.
    old=sql(f"SELECT row_to_json(t) FROM (SELECT monthly_usage_usd,expires_at FROM user_subscriptions WHERE id={target['subscription_id']}) t;")
    original=json.loads(old)
    try:
        sql(f"UPDATE user_subscriptions SET monthly_usage_usd=40 WHERE id={target['subscription_id']};")
        subprocess.run(['docker','exec','skoob-membership-pricing-redis','redis-cli','FLUSHDB'],check=True,stdout=subprocess.DEVNULL)
        chat(target['key'],'glm-5.3-flash',expected=(403,429))
        checks.append('40 元订阅额度耗尽后拒绝付费调用')
        sql(f"UPDATE user_subscriptions SET monthly_usage_usd=0,expires_at=now()-interval '1 second' WHERE id={target['subscription_id']};")
        subprocess.run(['docker','exec','skoob-membership-pricing-redis','redis-cli','FLUSHDB'],check=True,stdout=subprocess.DEVNULL)
        # JWT/API-key L1 may cache subscription status briefly. Expiry is evaluated from the persisted subscription.
        chat(target['key'],'glm-5.3-flash',expected=(401,403,429))
        checks.append('订阅到期后拒绝调用')
    finally:
        sql(f"UPDATE user_subscriptions SET monthly_usage_usd={original['monthly_usage_usd']},expires_at='{original['expires_at']}' WHERE id={target['subscription_id']};")
        subprocess.run(['docker','exec','skoob-membership-pricing-redis','redis-cli','FLUSHDB'],check=True,stdout=subprocess.DEVNULL)
    output={'simulation':True,'verified_at':time.strftime('%Y-%m-%d %H:%M:%S'),'checks':checks,'subscriptions':report,'usage':logs,'limitations':['模型响应和 tokens 是模拟值，未验证供应商可用性或质量','未发布线上套餐','界面部分 USD 标签仍需人民币展示适配','未验证 Skoob OAuth/兑换界面','GPT/Claude 未配置具体供货型号','同一付费 Key 的额度耗尽后，分组闸门也会拒绝免费模型；发布前待处理']}
    (STATE/'verification.json').write_text(json.dumps(output,ensure_ascii=False,indent=2))
    print(json.dumps({'checks':checks,'usage_rows':len(logs)},ensure_ascii=False))

class Mock(BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def send(self,obj):
        raw=json.dumps(obj).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
    def do_GET(self):
        self.send({'object':'list','data':[{'id':m,'object':'model','owned_by':'local-simulation'} for m in MODELS]})
    def do_POST(self):
        body=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))))
        self.send({'id':'chatcmpl-local-'+str(time.time_ns()),'object':'chat.completion','created':int(time.time()),'model':body.get('model'),'choices':[{'index':0,'message':{'role':'assistant','content':'OK（本地模拟响应）'},'finish_reason':'stop'}],'usage':{'prompt_tokens':1000,'completion_tokens':100,'total_tokens':1100,'prompt_tokens_details':{'cached_tokens':0}}})

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('action',choices=['mock','seed','check']);args=parser.parse_args()
    if args.action=='mock': ThreadingHTTPServer(('127.0.0.1',18442),Mock).serve_forever()
    elif args.action=='seed': seed()
    else: check()
