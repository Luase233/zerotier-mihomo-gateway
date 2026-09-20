'use strict';
const $ = id => document.getElementById(id);
let token='', state=null, groupName='', busy=false, loading=false, connected=false, pending=null, noticeTimer;
const storage={get(key){try{return localStorage.getItem(key)||sessionStorage.getItem(key)||''}catch{return ''}},save(value,remember){try{localStorage.removeItem('zb-token');sessionStorage.removeItem('zb-token');(remember?localStorage:sessionStorage).setItem('zb-token',value)}catch{}},clear(){try{localStorage.removeItem('zb-token');sessionStorage.removeItem('zb-token')}catch{}}};
function notify(message){$('notice').textContent=message;$('notice').hidden=false;clearTimeout(noticeTimer);noticeTimer=setTimeout(()=>$('notice').hidden=true,6500)}
function status(ok,text){connected=ok;$('connection').classList.toggle('online',ok);$('connectionText').textContent=text}
function page(name){for(const id of ['proxies','history','settings'])$(id).hidden=id!==name;document.querySelectorAll('[data-page]').forEach(b=>b.classList.toggle('active',b.dataset.page===name))}
async function api(path,body){const ctrl=new AbortController(),timer=setTimeout(()=>ctrl.abort(),25000);try{const res=await fetch(path,{method:body?'POST':'GET',headers:{Authorization:'Bearer '+token,...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined,cache:'no-store',signal:ctrl.signal});if(res.status===401)throw Error('访问密钥无效，请重新配对');if(!res.ok){let detail;try{detail=await res.json()}catch{}throw Error(detail?.error||'连接失败（'+res.status+'）')}return await res.json()}finally{clearTimeout(timer)}}
function element(tag,text,className){const el=document.createElement(tag);if(text!==undefined)el.textContent=text;if(className)el.className=className;return el}
function render(){
 $('mode').textContent=({global:'全局模式',rule:'规则模式',direct:'直连模式'})[state.mode]||state.mode;
 if(!state.groups.some(g=>g.name===groupName))groupName=state.groups.find(g=>g.name==='GLOBAL')?.name||state.groups[0]?.name||'';
 const select=$('groups');select.replaceChildren();for(const g of state.groups){const option=element('option',g.name+(g.selectable?'':' · 自动'));option.value=g.name;select.append(option)}select.value=groupName;select.disabled=busy;
 renderNodes();renderEvents();
}
function renderNodes(){
 const g=state?.groups.find(g=>g.name===groupName),q=$('search').value.toLocaleLowerCase();$('currentNode').textContent=g?.now||'未选择';
 $('groupHint').textContent=!g?'没有可用代理组':!g.selectable?'此组由 Clash 自动选择，可查看当前节点；请选择手动代理组进行切换。':state.mode==='global'&&g.name!=='GLOBAL'?'当前为全局模式。此组只有被 GLOBAL 选中或引用时才影响全局出口。':'点击下方节点，确认后切换；电脑面板同步显示命令结果。';
 const list=$('nodes');list.replaceChildren();const names=(g?.all||[]).filter(n=>n.toLocaleLowerCase().includes(q));$('nodeCount').textContent=names.length+' 项';
 for(const name of names){const selected=name===g.now,button=element('button',undefined,'node'+(selected?' selected':''));button.type='button';button.disabled=busy||!connected||!g.selectable||selected;button.append(element('span',undefined,'indicator'),element('span',name,'name'),element('small',selected?'当前使用':g.selectable?'选择':'自动'));button.addEventListener('click',()=>{pending={group:g.name,name};$('confirmGroup').textContent='代理组 · '+g.name;$('confirmNode').textContent=name;$('confirm').showModal()});list.append(button)}
 if(!names.length)list.append(element('div','没有匹配的节点','empty'));
}
const outcome=e=>({success:'切换成功 · Clash 已确认',pending:'命令执行中',unconfirmed:'结果待确认，请刷新当前选择'})[e.status]||e.status;
function renderEvents(){const list=$('events');list.replaceChildren();for(const e of [...(state.events||[])].reverse()){const card=element('article',undefined,'event'),row=element('div',undefined,'row');row.append(element('span',new Date(e.time).toLocaleTimeString()),element('span',e.status==='success'?'已同步':'待确认','pill'));card.append(row,element('strong',e.group+' → '+e.target),element('p','原选择：'+e.before),element('p',outcome(e),e.status==='success'?'result':'warning'),element('p','来源 '+e.client));list.append(card)}if(!state.events?.length)list.append(element('div','还没有切换记录。手机命令将在这里和电脑端同时显示。','empty'))}
async function refresh(silent=false){if(!token||loading||busy)return;loading=true;try{state=await api('/api/state');status(state.online,state.online?'已连接电脑 · '+new Date(state.updated).toLocaleTimeString():'电脑在线 · Clash 暂不可用');$('login').hidden=true;$('tabs').hidden=false;if($('proxies').hidden&&$('history').hidden&&$('settings').hidden)page('proxies');render();if(!state.online&&!silent)notify('Clash 暂不可用，请检查电脑上的 Clash')}catch(e){status(false,'未连接 · 请检查 ZeroTier 和电脑');if(state)renderNodes();if(!silent)notify(e.message);if(e.message.includes('密钥')){token='';storage.clear();$('login').hidden=false;$('tabs').hidden=true;for(const id of ['proxies','history','settings'])$(id).hidden=true}}finally{loading=false}}
$('loginForm').addEventListener('submit',async e=>{e.preventDefault();token=$('token').value.trim();storage.save(token,$('remember').checked);await refresh();if(connected)$('token').value=''});
$('groups').addEventListener('change',()=>{groupName=$('groups').value;renderNodes()});$('search').addEventListener('input',renderNodes);$('refresh').addEventListener('click',()=>refresh());$('cancel').addEventListener('click',()=>$('confirm').close());
$('apply').addEventListener('click',async()=>{if(busy||!pending)return;const command=pending;pending=null;$('confirm').close();busy=true;render();const bytes=new Uint8Array(16);crypto.getRandomValues(bytes);const id=Array.from(bytes,b=>b.toString(16).padStart(2,'0')).join('');try{const result=await api('/api/select',{id,group:command.group,name:command.name});notify(outcome(result));}catch(e){notify('命令结果未确认：'+e.message+'。请先查看记录和当前选择。')}finally{busy=false;await refresh(true)}});
document.querySelectorAll('[data-page]').forEach(b=>b.addEventListener('click',()=>page(b.dataset.page)));
$('logout').addEventListener('click',()=>{storage.clear();token='';state=null;status(false,'已忘记此设备');$('login').hidden=false;$('tabs').hidden=true;for(const id of ['proxies','history','settings'])$(id).hidden=true});
$('address').textContent=location.origin;
const fragment=new URLSearchParams(location.hash.slice(1));if(fragment.has('token')){$('token').value=fragment.get('token');history.replaceState(null,'',location.pathname)}else{token=storage.get('zb-token');if(token)refresh()}
setInterval(()=>{if(!document.hidden)refresh(true)},5000);document.addEventListener('visibilitychange',()=>{if(!document.hidden)refresh(true)});
