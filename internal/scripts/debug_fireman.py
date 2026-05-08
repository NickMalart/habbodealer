import binascii, re
s='405c495143615a774d466972656d616e0268722d3131352d313033352e68642d3138302d313032352e63682d3235352d313231382e6c672d3238302d313138392e73682d3330352d313236372e65612d313430342d31313839026d02533e206f73727320677020666f72206f726967696e730250415341302e30020202497374640273746402504148534e'
b=binascii.unhexlify(s)
print('len',len(b))
print('preview',b[:40])
delim=b'\x02hr-'
idx=b.find(delim)
print('delim idx',idx)
name_end=idx
pre_start=max(0,name_end-64)
window_bytes=b[pre_start:name_end]
window_str=window_bytes.decode('latin-1')
print('window_str',window_str)
name_pat=re.compile(r'([A-Za-z][A-Za-z0-9_-]{2,})$')
fallback_pat=re.compile(r'([A-Za-z][A-Za-z0-9_-]{1,})$')
m=name_pat.search(window_str) or fallback_pat.search(window_str)
print('match object',m)
if m:
    raw_name_start=pre_start+m.start(1)
    print('raw_name_start',raw_name_start)
    adj=raw_name_start
    if raw_name_start+1 < len(b):
        b0,b1=b[raw_name_start], b[raw_name_start+1]
        print('b0,b1',b0,b1,chr(b0),chr(b1))
        if 65 <= b0 <= 90 and 65 <= b1 <= 90:
            adj = raw_name_start + 1
    print('adj',adj)
    name_bytes=b[adj:name_end]
    print('name_bytes',name_bytes)
    try:
        name=name_bytes.decode('utf-8')
    except Exception:
        name=name_bytes.decode('latin-1',errors='replace')
    print('name',name)
    print('adj<4?',adj<4)
    if adj>=4:
        token_start=adj-4
        print('token_start',token_start)
        print('token_bytes',b[token_start:adj])
        # scan for VL64 finishing at token_start
        room_index=0
        scan_start=max(0, token_start-6)
        for start_off in range(scan_start, token_start):
            try:
                first=b[start_off]
                vlen=(first >> 3) & 7
                if vlen>0 and vlen<=6 and start_off+vlen==token_start:
                    chunk=b[start_off:token_start]
                    # naive vl64 decode
                    value=int(chunk[0] & 3)
                    n=vlen
                    for i in range(1,n):
                        value |= int(chunk[i] & 0x3f) << (2 + 6*(i-1))
                    if chunk[0] & 4:
                        value *= -1
                    if value>0:
                        room_index=value
                        break
            except Exception:
                pass
        print('room_index',room_index)
else:
    print('no regex match')
