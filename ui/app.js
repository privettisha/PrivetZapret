const token=new URLSearchParams(location.search).get('token')||'';
const $=s=>document.querySelector(s);
let status=null,busy=false,dirty=false,settingsDirty=false,draft=new Set(),search='',diagnostics=null,systemReport=null,profileDraft={};

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
  else if(s.needs_update){$('#state').textContent='Доступно обновление';$('#subtitle').textContent='Настройки и ключ Telegram сохранятся.';btn.textContent='Запустить';btn.className='primary';$('#removeButton').hidden=false}
  else if(s.active){$('#state').textContent='Подключение активно';$('#subtitle').textContent=`Локально обрабатывается: ${plural((s.services||[]).length)}. Остальной трафик идёт напрямую.`;btn.textContent='Остановить';btn.className='primary stop';$('#removeButton').hidden=false}
  else{$('#state').textContent='Подключение остановлено';$('#subtitle').textContent='Выбор сохранён. Нажмите кнопку, чтобы снова включить.';btn.textContent='Запустить';btn.className='primary';$('#removeButton').hidden=false}
  btn.disabled=busy;
  $('#checkUpdate').hidden=!s.installed;
  apply.disabled=busy||!dirty;apply.textContent=dirty?'Применить выбор':'Изменений нет';
  const telegramSelected=draft.has('telegram');tg.hidden=!telegramSelected;tg.disabled=busy||!s.telegram_ready;
  $('#youtubePanel').hidden=!draft.has('youtube');
  $('#checkYouTube').disabled=busy||!s.active;
  $('#autoYouTube').disabled=busy||!s.active;
  document.querySelectorAll('[data-youtube-profile]').forEach(button=>{
    button.classList.toggle('selected',button.dataset.youtubeProfile===s.youtube_profile);
    button.setAttribute('aria-pressed',String(button.dataset.youtubeProfile===s.youtube_profile));
    button.disabled=busy||!s.installed;
  });
  renderDiagnostics();
  if(!settingsDirty){
    profileDraft={...(s.profiles||{})};
    $('#customDomains').value=(s.custom_domains||[]).join('\n');
    $('#excludeDomains').value=(s.exclude_domains||[]).join('\n');
    $('#autoHostlist').checked=!!s.auto_hostlist;
    $('#gameMode').checked=!!s.game_mode;
    $('#autoUpdate').checked=!!s.auto_update;
    $('#discordProfile').value=s.profiles?.discord||'standard';
    $('#genericProfile').value=s.profiles?.generic||'standard';
  }
  $('#saveSettings').disabled=busy||!s.installed;
  $('#repairButton').disabled=busy||!s.installed;
  $('#autoDiscord').disabled=busy||!s.active||!draft.has('discord');
  $('#rollbackButton').hidden=!s.rollback_available;
  renderCatalog(s);
  renderServiceProfiles(s);
}
function renderServiceProfiles(s){
  const root=$('#serviceProfiles');root.textContent='';
  for(const service of s.catalog||[]){
    if(!draft.has(service.id)||['youtube','discord','telegram'].includes(service.id))continue;
    const row=document.createElement('div');row.className='service-profile';
    const label=document.createElement('label');label.className='settings-field';label.textContent=service.name;
    const select=document.createElement('select');
    for(const [value,name] of [['standard','Стандарт'],['split','Разделение'],['alternative','Альтернативный'],['tcp','Без QUIC']]){
      const option=document.createElement('option');option.value=value;option.textContent=name;select.append(option);
    }
    select.value=profileDraft[service.id]||profileDraft.generic||'standard';select.disabled=busy||!s.installed;
    select.onchange=()=>{profileDraft[service.id]=select.value;settingsDirty=true};label.append(select);
    const auto=document.createElement('button');auto.type='button';auto.className='text-action';auto.textContent='Подобрать';auto.disabled=busy||!s.active;
    auto.onclick=()=>act(async()=>{await api('/api/strategies/auto',{method:'POST',body:JSON.stringify({service:service.id})});settingsDirty=false;toast('Профиль выбран')});
    row.append(label,auto);root.append(row);
  }
}
function renderDiagnostics(){
  const root=$('#youtubeResults');root.textContent='';root.hidden=!diagnostics;
  if(!diagnostics)return;
  for(const [key,label] of [['youtube','Сайт'],['preview','Превью'],['video_server','Видеосервер'],['soundcloud_media_server','SoundCloud CDN']]){
    const check=diagnostics[key];
    if(key==='soundcloud_media_server'&&!draft.has('soundcloud'))continue;
    const row=document.createElement('div');row.className='diagnostic-row';
    const name=document.createElement('span');name.textContent=label;
    const value=document.createElement('strong');const usable=check?.reachable&&(check.http_code>=200&&check.http_code<400||key==='soundcloud_media_server'&&check.http_code===403);
    value.className=usable?'ok':'failed';
    value.textContent=usable?(key==='soundcloud_media_server'?'CDN отвечает':'Доступно'):(check?.reachable?`HTTP ${check.http_code}`:check?.error||'Нет соединения');
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
$('#autoYouTube').onclick=()=>act(async()=>{await api('/api/strategies/auto',{method:'POST',body:JSON.stringify({service:'youtube'})});diagnostics=await api('/api/diagnostics')});
$('#autoDiscord').onclick=()=>act(async()=>{await api('/api/strategies/auto',{method:'POST',body:JSON.stringify({service:'discord'})});settingsDirty=false;toast('Профиль выбран')});
for(const control of document.querySelectorAll('.settings-panel textarea,.settings-panel select,.settings-panel input'))control.oninput=()=>{settingsDirty=true};
$('#saveSettings').onclick=()=>act(async()=>{
  const lines=id=>$('#'+id).value.split(/[\n,;]+/).map(x=>x.trim()).filter(Boolean);
  await api('/api/settings',{method:'POST',body:JSON.stringify({custom_domains:lines('customDomains'),exclude_domains:lines('excludeDomains'),auto_hostlist:$('#autoHostlist').checked,game_mode:$('#gameMode').checked,auto_update:$('#autoUpdate').checked,profiles:{...profileDraft,discord:$('#discordProfile').value,generic:$('#genericProfile').value}})});
  settingsDirty=false;toast('Сохранено');
});
$('#repairButton').onclick=()=>act(async()=>{await api('/api/repair',{method:'POST'});toast('Готово')});
$('#systemCheck').onclick=()=>act(async()=>{systemReport=await api('/api/system');renderSystem()});
function renderSystem(){
  const root=$('#systemResults');root.textContent='';root.hidden=!systemReport;
  $('#copyReport').hidden=!systemReport;
  if(!systemReport)return;
  for(const [label,value] of [['Служба',systemReport.active?'Работает':'Остановлена'],['Другие фильтры',(systemReport.conflicts||[]).join(', ')||'Не найдены'],['VPN',(systemReport.vpn_interfaces||[]).join(', ')||'Не найден'],['YouTube',systemReport.endpoints?.youtube?.reachable?'Сайт отвечает':systemReport.endpoints?.youtube?.error||'Нет соединения'],['YouTube CDN',systemReport.endpoints?.video_server?.reachable?'Сервер отвечает':systemReport.endpoints?.video_server?.error||'Нет соединения'],['SoundCloud CDN',systemReport.endpoints?.soundcloud_media_server?.reachable?'Сервер отвечает':systemReport.endpoints?.soundcloud_media_server?.error||'Нет соединения']]){
    const row=document.createElement('div'),a=document.createElement('span'),b=document.createElement('strong');a.textContent=label;b.textContent=value;row.append(a,b);root.append(row);
  }
}
$('#copyReport').onclick=async()=>{if(!systemReport)return;await navigator.clipboard.writeText(JSON.stringify(systemReport,null,2));toast('Отчёт скопирован')};
$('#checkUpdate').onclick=async()=>{
  if(busy)return;
  const button=$('#checkUpdate'),previous=button.textContent;
  busy=true;button.disabled=true;button.textContent='Проверяем…';
  try{
    const update=await api('/api/update/check');
    if(!update.available){toast('Установлена актуальная версия');return}
    button.textContent='Устанавливаем…';
    await api('/api/update/apply',{method:'POST'});
    toast('Обновление запущено. Откройте приложение снова.');
  }catch(error){toast(error.message)}
  finally{button.textContent=previous;button.disabled=false;busy=false}
};
$('#rollbackButton').onclick=()=>act(async()=>{await api('/api/update/rollback',{method:'POST'});toast('Восстановление запущено')});
$('#serviceSearch').oninput=e=>{search=e.target.value;if(status)renderCatalog(status)};
document.querySelectorAll('[data-preset]').forEach(button=>button.onclick=()=>{if(!status)return;const preset=button.dataset.preset;if(preset==='all')draft=new Set(status.catalog.map(item=>item.id));else if(preset==='default')draft=new Set(status.catalog.filter(item=>item.default).map(item=>item.id));else draft=new Set(status.services);dirty=!sameSelection(draft,status.services||[]);render(status)});
refresh();
