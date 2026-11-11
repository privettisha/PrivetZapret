const token=new URLSearchParams(location.search).get('token')||'';
const $=s=>document.querySelector(s);
let status=null,busy=false,dirty=false,draft=new Set(),search='',diagnostics=null;

async function api(path,options={}){
  const r=await fetch(path,{...options,headers:{'Content-Type':'application/json','X-Privet-Token':token,...options.headers}});
  const j=await r.json();
  if(!r.ok)throw new Error(j.error||'Не удалось выполнить действие');
  return j;
}
function toast(text){const e=$('#toast');e.textContent=text;e.classList.add('show');setTimeout(()=>e.classList.remove('show'),2600)}
function plural(n){const m=n%100;if(m>=11&&m<=14)return`${n} сервисов`;switch(n%10){case 1:return`${n} сервис`;case 2:case 3:case 4:return`${n} сервиса`;default:return`${n} сервисов`}}
function sameSelection(a,b){return a.size===b.length&&b.every(id=>a.has(id))}

function renderCatalog(s){
  const root=$('#serviceCatalog');root.textContent='';
  const query=search.trim().toLocaleLowerCase('ru');
  const groups=new Map();
  for(const service of s.catalog||[]){
    if(query&&!`${service.name} ${service.description} ${service.category}`.toLocaleLowerCase('ru').includes(query))continue;
    if(!groups.has(service.category))groups.set(service.category,[]);
    groups.get(service.category).push(service);
  }
  for(const [category,services] of groups){
    const section=document.createElement('section');section.className='service-group';
    const heading=document.createElement('h3');heading.textContent=category;section.append(heading);
    const grid=document.createElement('div');grid.className='service-grid';
    for(const service of services){
      const label=document.createElement('label');label.className='service-card'+(draft.has(service.id)?' selected':'');
      const input=document.createElement('input');input.type='checkbox';input.checked=draft.has(service.id);input.disabled=busy;
      const copy=document.createElement('span');const name=document.createElement('b');name.textContent=service.name;
      const description=document.createElement('small');description.textContent=service.description;copy.append(name,description);
      const control=document.createElement('i');control.setAttribute('aria-hidden','true');
      input.onchange=()=>{input.checked?draft.add(service.id):draft.delete(service.id);if(draft.size===0){draft.add(service.id);input.checked=true;toast('Нужно оставить хотя бы один сервис')}dirty=!sameSelection(draft,s.services||[]);render(s)};
      label.append(input,copy,control);grid.append(label);
    }
    section.append(grid);root.append(section);
  }
  if(!groups.size){const empty=document.createElement('p');empty.className='catalog-empty';empty.textContent='Ничего не найдено';root.append(empty)}
}

function render(s){
  status=s;
  if(!dirty)draft=new Set(s.services||[]);
  $('#platform').textContent=s.platform;
  const orb=$('#orb'),btn=$('#mainButton'),tg=$('#telegramButton'),apply=$('#applyServices');
  const count=draft.size;$('#selectedCount').textContent=`${plural(count)} выбрано`;$('#routeServices').textContent=plural(count);
  orb.className='orb '+(s.active?'active':'inactive');
  if(!s.installed){$('#state').textContent='Готов к установке';$('#subtitle').textContent='Выберите нужные сервисы и установите приложение.';btn.textContent='Установить';btn.className='primary';$('#removeButton').hidden=true}
  else if(s.needs_update){$('#state').textContent='Доступно обновление';$('#subtitle').textContent='Настройки и ключ Telegram сохранятся.';btn.textContent='Обновить';btn.className='primary';$('#removeButton').hidden=false}
  else if(s.active){$('#state').textContent='Подключение активно';$('#subtitle').textContent=`Локально обрабатывается: ${plural((s.services||[]).length)}. Остальной трафик идёт напрямую.`;btn.textContent='Остановить';btn.className='primary stop';$('#removeButton').hidden=false}
  else{$('#state').textContent='Подключение остановлено';$('#subtitle').textContent='Выбор сохранён. Нажмите кнопку, чтобы снова включить.';btn.textContent='Запустить';btn.className='primary';$('#removeButton').hidden=false}
  btn.disabled=busy;
  apply.disabled=busy||!dirty;apply.textContent=dirty?'Применить выбор':'Изменений нет';
  const telegramSelected=draft.has('telegram');tg.hidden=!telegramSelected;tg.disabled=busy||!s.telegram_ready;
  $('#youtubePanel').hidden=!draft.has('youtube');
  $('#checkYouTube').disabled=busy||!s.active;
  document.querySelectorAll('[data-youtube-profile]').forEach(button=>{
    button.classList.toggle('selected',button.dataset.youtubeProfile===s.youtube_profile);
    button.setAttribute('aria-pressed',String(button.dataset.youtubeProfile===s.youtube_profile));
    button.disabled=busy||!s.installed;
  });
  renderDiagnostics();
  renderCatalog(s);
}
function renderDiagnostics(){
  const root=$('#youtubeResults');root.textContent='';root.hidden=!diagnostics;
  if(!diagnostics)return;
  for(const [key,label] of [['youtube','Сайт'],['preview','Превью']]){
    const check=diagnostics[key];
    const row=document.createElement('div');row.className='diagnostic-row';
    const name=document.createElement('span');name.textContent=label;
    const value=document.createElement('strong');value.className=check?.reachable?'ok':'failed';
    value.textContent=check?.reachable?(check.http_code>=400?`HTTP ${check.http_code}`:'Доступно'):(check?.error||'Нет соединения');
    row.append(name,value);root.append(row);
  }
}
async function refresh(){try{render(await api('/api/status'))}catch(e){toast(e.message)}}
async function act(fn){if(busy)return;busy=true;if(status)render(status);try{await fn();await refresh()}catch(e){toast(e.message)}finally{busy=false;if(status)render(status)}}

$('#mainButton').onclick=()=>act(async()=>{if(!status.installed){await api('/api/install',{method:'POST'});if(dirty){await api('/api/services',{method:'POST',body:JSON.stringify({services:[...draft]})});dirty=false}}else if(status.needs_update)await api('/api/install',{method:'POST'});else if(status.active)await api('/api/stop',{method:'POST'});else await api('/api/start',{method:'POST'})});
$('#removeButton').onclick=()=>{if(confirm('Удалить PrivetZapret и вернуть обычную работу сети?'))act(()=>api('/api/remove',{method:'POST'}))};
$('#telegramButton').onclick=()=>act(async()=>{await api('/api/telegram/connect',{method:'POST'});toast('Подтвердите подключение в Telegram Desktop')});
$('#applyServices').onclick=()=>act(async()=>{const next=[...draft];await api('/api/services',{method:'POST',body:JSON.stringify({services:next})});dirty=false;toast('Выбор применён')});
document.querySelectorAll('[data-youtube-profile]').forEach(button=>button.onclick=()=>act(async()=>{if(button.dataset.youtubeProfile===status.youtube_profile)return;await api('/api/youtube/profile',{method:'POST',body:JSON.stringify({profile:button.dataset.youtubeProfile})});diagnostics=null}));
$('#checkYouTube').onclick=()=>act(async()=>{diagnostics=await api('/api/diagnostics')});
$('#serviceSearch').oninput=e=>{search=e.target.value;if(status)renderCatalog(status)};
document.querySelectorAll('[data-preset]').forEach(button=>button.onclick=()=>{if(!status)return;const preset=button.dataset.preset;if(preset==='all')draft=new Set(status.catalog.map(item=>item.id));else if(preset==='default')draft=new Set(status.catalog.filter(item=>item.default).map(item=>item.id));else draft=new Set(status.services);dirty=!sameSelection(draft,status.services||[]);render(status)});
refresh();
