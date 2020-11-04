#!/usr/bin/env python3

import sys
import requests
import time

# TODO: Check all return values and assert/bailout on error

url = "http://" + sys.argv[1] + ":8080/api/v1/"
tmpdir = sys.argv[2]

def is_controller_up():
    try:
        ret = requests.get(url+"getalltenants")
        return (ret.status_code == 200)
    except:
        pass
        return False

def create_gateway(gw):
    data = {'name': gw}
    try:
        ret = requests.post(url+"addgateway", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def create_tenant(name, gws, domains, image, pods):
    data = {'curid': 'unknown', 'name': name, 'gateways': gws, 
            'domains': domains, 'image': image, 'pods': pods}
    try:
        ret = requests.post(url+"addtenant", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def get_tenants():
    try:
        ret = requests.get(url+"getalltenants")
        if ret.status_code != 200:
            return False, json.dumps([])
        return True, ret.json()
    except:
        pass
        return False

def create_user(uid, tenant, name, email, services, gateway):
    data = {'uid': uid, 'tenant': tenant, 'name': name, 'email': email, 'services': services, 'gateway': gateway}
    try:
        ret = requests.post(url+"adduser", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def create_bundle(bid, tenant, name, services, gateway):
    data = {'bid': bid, 'tenant': tenant, 'name': name, 'services': services, 'gateway': gateway}
    try:
        ret = requests.post(url+"addbundle", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False        

def create_user_attr(uid, tenant, category, type, level, dept, team):
    data = {'uid': uid, 'tenant': tenant, 'category': category, 'type': type, 'level': level,
            'dept': dept, 'team': team}
    try:
        ret = requests.post(url+"adduserattr", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def create_bundle_attr(bid, tenant, dept, team, IC, manager, nonemployee):
    data = {'bid': bid, 'tenant': tenant, 'IC': IC, 'manager': manager,
            'nonemployee': nonemployee, 'dept': dept, 'team': team}
    try:
        ret = requests.post(url+"addbundleattr", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def create_policy(tenant, pid, policy):
    rego = []
    for p in policy:
        rego.append(ord(p))
    data = {'tenant': tenant, 'pid': pid, 'rego': rego}
    try:
        ret = requests.post(url+"addpolicy", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def create_route(tenant, user, route, tag):
    data = {'tenant': tenant, 'route': user + ":" + route, 'tag': tag}
    try:
        ret = requests.post(url+"addroute", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

def create_cert():
    f = open(tmpdir+"/rootca.crt", 'r')
    cert = f.read()
    f.close()
    data = {'certid': 'CACert', 'cert': [ord(c) for c in cert]}
    try:
        ret = requests.post(url+"addcert", json=data)
        if ret.status_code != 200 or ret.json()['Result'] != "ok":
            return False
        return True
    except:
        pass
        return False

if __name__ == '__main__':
    while not is_controller_up():
        print('Controller not up, waiting ...')
        time.sleep(5)

    ok = create_gateway("gateway.testa.nextensio.net")
    while not ok:
        print('Gateway creation failed, retrying ...')
        time.sleep(1)
        ok = create_gateway("gateway.testa.nextensio.net")

    ok = create_gateway("gateway.testc.nextensio.net")
    while not ok:
        print('Gateway creation failed, retrying ...')
        time.sleep(1)
        ok = create_gateway("gateway.testc.nextensio.net")

    ok = create_tenant("Test", ["gateway.testa.nextensio.net","gateway.testc.nextensio.net"], 
                       ["kismis.org"], "registry.gitlab.com/nextensio/cluster/minion:latest", 5)
    while not ok:
        print('Tenant creation failed, retrying ...')
        time.sleep(1)
        ok = create_tenant("Test", ["gateway.testa.nextensio.net","gateway.testc.nextensio.net"], 
                           ["kismis.org"], "registry.gitlab.com/nextensio/cluster/minion:latest", 5)

    ok, tenants = get_tenants()
    while not ok:
        print('Tenant creation failed, retrying ...')
        time.sleep(1)
        ok, tenants = get_tenants()

    # The test setup is assumed to be created with just one tenant, if we need more we just need
    # to search for the right tenant name or something inside the returned list of tenants
    tenant = tenants[0]['_id']

    ok = create_user('test1@nextensio.net', tenant, 'Test User1', 'test1@nextensio.net', ['test1-nextensio-net'], 'gateway.testa.nextensio.net')
    while not ok:
        print('User creation failed, retrying ...')
        time.sleep(1)
        ok = create_user('test1@nextensio.net', tenant, 'Test User1', 'test1@nextensio.net', ['test1-nextensio-net'], 'gateway.testa.nextensio.net')

    ok = create_user_attr('test1@nextensio.net', tenant, 'employee', 'IC', 50, ['ABU,BBU'], ['engineering','sales'])
    while not ok:
        print('UserAttr creation failed, retrying ...')
        time.sleep(1)
        ok = create_user_attr('test1@nextensio.net', tenant, 'employee', 'IC', 50, ['ABU,BBU'], ['engineering','sales'])
    
    ok = create_user('test2@nextensio.net', tenant, 'Test User2', 'test2@nextensio.net', ['test2-nextensio-net'], 'gateway.testa.nextensio.net')
    while not ok:
        print('User creation failed, retrying ...')
        time.sleep(1)
        ok = create_user('test2@nextensio.net', tenant, 'Test User2', 'test2@nextensio.net', ['test2-nextensio-net'], 'gateway.testa.nextensio.net')
    
    ok = create_user_attr('test2@nextensio.net', tenant, 'employee', 'IC', 50, ['ABU,BBU'], ['engineering','sales'])
    while not ok:
        print('UserAttr creation failed, retrying ...')
        time.sleep(1)
        ok = create_user_attr('test2@nextensio.net', tenant, 'employee', 'IC', 50, ['ABU,BBU'], ['engineering','sales'])

    ok = create_bundle('default@nextensio.net', tenant, 'Default Internet Route', ['default-internet'], 'gateway.testc.nextensio.net')
    while not ok:
        print('Bundle creation failed, retrying ...')
        time.sleep(1)
        ok = create_bundle('default@nextensio.net', tenant, 'Default Internet Route', ['default-internet'], 'gateway.testc.nextensio.net')

    ok = create_bundle_attr('default@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")
    while not ok:
        print('BundleAttr creation failed, retrying ...')
        time.sleep(1)
        ok = create_bundle_attr('default@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")

    ok = create_bundle('v1.kismis@nextensio.net', tenant, 'Kismis Version ONE', ['v1.kismis.org'], 'gateway.testc.nextensio.net')
    while not ok:
        print('Bundle creation failed, retrying ...')
        time.sleep(1)
        ok = create_bundle('v1.kismis@nextensio.net', tenant, 'Kismis Version ONE', ['v1.kismis.org'], 'gateway.testc.nextensio.net')

    ok = create_bundle_attr('v1.kismis@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")
    while not ok:
        print('BundleAttr creation failed, retrying ...')
        time.sleep(1)
        ok = create_bundle_attr('v1.kismis@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")

    ok = create_bundle('v2.kismis@nextensio.net', tenant, 'Kismis Version ONE', ['v2.kismis.org'], 'gateway.testc.nextensio.net')
    while not ok:
        print('Bundle creation failed, retrying ...')
        time.sleep(1)
        ok = create_bundle('v2.kismis@nextensio.net', tenant, 'Kismis Version ONE', ['v2.kismis.org'], 'gateway.testc.nextensio.net')
        
    ok = create_bundle_attr('v2.kismis@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")
    while not ok:
        print('BundleAttr creation failed, retrying ...')
        time.sleep(1)
        ok = create_bundle_attr('v2.kismis@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")

    ok = create_route(tenant, 'test1@nextensio.net', 'kismis.org', 'v1')
    while not ok:
        print('Route creation failed, retrying ...')
        time.sleep(1)
        ok = create_route(tenant, 'test1@nextensio.net', 'kismis.org', 'v1')
        
    ok = create_route(tenant, 'test2@nextensio.net', 'kismis.org', 'v2')
    while not ok:
        print('Route creation failed, retrying ...')
        time.sleep(1)
        ok = create_route(tenant, 'test2@nextensio.net', 'kismis.org', 'v2')

    with open('policy.AccessPolicy','r') as file:
        rego = file.read()
    ok = create_policy(tenant, 'AccessPolicy', rego)
    while not ok:
        print('Policy creation failed, retrying ...')
        time.sleep(1)
        ok = create_policy(tenant, 'AccessPolicy', rego)

    ok = create_cert()
    while not ok:
        print('CERT creation failed, retrying ...')
        time.sleep(1)
        ok = create_cert()
