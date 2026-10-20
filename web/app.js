'use strict';
const $ = id => document.getElementById(id);
const state = { user:null, event:null, seats:[], selected:null, mode:'login', route:'', busy:false, request:null, bookings:[], cursor:0, offset:0, generation:0 };
const money = cents => new Intl.NumberFormat('en-KZ',{style:'currency',currency:'KZT',maximumFractionDigits:2}).format(cents/100);
const date = value => new Date(value).toLocaleString(undefined,{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'});
function el(tag,cls,text){const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n;}
let toastTimer;
function toast(message){$('toast').textContent=message;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,4500);}
async function api(path,options={}){
 const headers={...(options.body?{'Content-Type':'application/json'}:{}),...options.headers};
 const response=await fetch(path,{credentials:'same-origin',...options,headers});
 const serverDate=Date.parse(response.headers.get('Date'));if(Number.isFinite(serverDate))state.offset=serverDate-Date.now();
 const data=await response.json();
 if(!response.ok){const err=new Error(data.error||`Request failed (${response.status})`);err.status=response.status;throw err;}return data;
}
function updateAccount(){
 $('auth-open').hidden=!!state.user;$('logout').hidden=!state.user;$('account-email').textContent=state.user?.email||'';
 $('admin-link').hidden=state.user?.role!=='admin';
 $('reserve-note').textContent=state.user?'No card needed. This is a demo checkout.':'Sign in to make a reservation.';
}
function showView(name){for(const v of ['events','detail','bookings','admin'])$(v+'-view').hidden=v!==name;for(const v of ['events','bookings','admin'])$(v+'-link').classList.toggle('active',v===name);}
function errorPanel(target,error){target.replaceChildren(el('div','error-panel',error.message));}
async function route(){
 const gen=++state.generation;const hash=location.hash||'#events';state.route=hash;
 try{
  if(/^#event\/\d+$/.test(hash)){
   showView('detail');$('detail-title').textContent='Loading event…';$('seat-grid').replaceChildren();$('reserve').disabled=true;
   state.selected=null;state.request=null;state.event=null;
   await loadDetail(Number(hash.split('/')[1]),gen);return;
  }
  if(hash==='#bookings'){
   showView('bookings');state.bookings=[];state.cursor=0;$('more-bookings').hidden=true;
   if(!state.user){$('bookings-list').replaceChildren(el('div','empty','Sign in to see your bookings.'));openAuth();return;}await loadBookings(false,gen);return;
  }
  if(hash==='#admin'){
   if(state.user?.role!=='admin'){location.hash='#events';toast('Organizer access is required.');return;}showView('admin');return;
  }
  showView('events');await loadEvents(gen);
 }catch(error){if(gen!==state.generation)return;toast(error.message);if(hash.startsWith('#event/')){$('detail-title').textContent='Event unavailable';$('seat-grid').replaceChildren(el('p','error-panel',error.message));}else errorPanel(hash==='#bookings'?$('bookings-list'):$('events-grid'),error);}
}
async function loadEvents(gen){
 const events=await api('/api/events');if(gen!==state.generation)return;
 $('event-count').textContent=`${events.length} upcoming events`;
 const cards=events.map((event,i)=>{
  const card=el('a','event-card');card.href='#event/'+event.id;
  const cover=el('div','event-cover theme-'+i%3);cover.append(el('span','event-badge','SEATBOOK / LIVE'),el('span','cover-word',['Meet.\nBuild.','Think.\nBigger.','Stay.\nListen.'][i%3]),el('span','cover-decor',['✳','↗','◎'][i%3]));
  const body=el('div','event-body');body.append(el('span','event-date',date(event.starts_at)),el('h3','',event.title),el('span','quiet',`${event.available_seats} / ${event.total_seats} seats available`));
  const bottom=el('div','event-card-bottom');bottom.append(el('strong','',money(event.price_cents)),el('span','arrow','↗'));body.append(bottom);card.append(cover,body);return card;
 });$('events-grid').replaceChildren(...(cards.length?cards:[el('div','empty','The next lineup is on its way. Check back soon.')]));
}
let detailLoading=false;
async function loadDetail(id,gen){
 const data=await api('/api/events/'+id);if(gen!==state.generation)return;
 state.event=data.event;state.seats=data.seats;
 $('detail-title').textContent=data.event.title;$('detail-meta').textContent=`${date(data.event.starts_at)} · ${data.event.available_seats} seats available`;
 $('detail-price').textContent=money(data.event.price_cents);$('detail-description').textContent=data.event.description;
 if(state.selected&&!state.seats.some(s=>s.id===state.selected&&s.status==='available')){state.selected=null;state.request=null;}
 renderSeats();updateSelection();
}
function renderSeats(){
 const rows=new Map();for(const seat of state.seats){if(!rows.has(seat.row))rows.set(seat.row,[]);rows.get(seat.row).push(seat);}
 const nodes=[];for(const [row,seats] of rows){const n=el('div','seat-row');n.append(el('span','row-label',String.fromCharCode(64+row)));
  for(const seat of seats){const b=el('button','seat '+seat.status+(seat.id===state.selected?' is-selected':''),String(seat.number));b.type='button';b.disabled=seat.status!=='available'||state.busy||new Date(state.event.starts_at).getTime()<=Date.now()+state.offset;b.setAttribute('aria-label',`Row ${String.fromCharCode(64+row)}, seat ${seat.number}: ${seat.status}`);b.setAttribute('aria-pressed',String(seat.id===state.selected));b.onclick=()=>{state.selected=seat.id;state.request=null;renderSeats();updateSelection();};n.append(b);}nodes.push(n);
 }$('seat-grid').replaceChildren(...nodes);
}
function updateSelection(){const seat=state.seats.find(s=>s.id===state.selected);$('selection-label').textContent=seat?`Row ${String.fromCharCode(64+seat.row)} · Seat ${seat.number}`:'Choose a seat';$('selection-price').textContent=state.event?money(state.event.price_cents):'—';$('reserve').disabled=!seat||state.busy;$('reserve').textContent=state.busy?'Reserving…':'Reserve this seat ↗';}
$('reserve').onclick=async()=>{
 if(!state.user){openAuth();return;}if(state.busy||!state.selected)return;
 const gen=state.generation;const seat=state.selected;
 // Keep the key on a network/server failure so the user can safely retry.
 if(!state.request||state.request.seat!==seat)state.request={seat,key:crypto.randomUUID()};
 state.busy=true;renderSeats();updateSelection();
 try{const b=await api('/api/bookings',{method:'POST',headers:{'Idempotency-Key':state.request.key},body:JSON.stringify({seat_id:seat})});state.request=null;state.selected=null;toast(b.status==='pending'?'Seat held! Confirm it before the timer ends.':`Reservation status: ${b.status}`);location.hash='#bookings';}
 catch(error){toast(error.message);if(error.status&&error.status<500){state.request=null;}if(error.status===401){state.user=null;updateAccount();openAuth();}if(gen===state.generation&&state.event){try{await loadDetail(state.event.id,gen);}catch(e){toast(e.message);}}}
 finally{state.busy=false;if(gen===state.generation&&state.event){renderSeats();updateSelection();}}
};
async function loadBookings(append,gen=state.generation){
 const list=await api('/api/bookings'+(append&&state.cursor?'?before='+state.cursor:''));if(gen!==state.generation)return;
 state.bookings=append?[...state.bookings,...list]:list;state.cursor=list.at(-1)?.id||0;$('more-bookings').hidden=list.length<100;renderBookings();
}
function renderBookings(){
 const cards=state.bookings.map(b=>{
  const card=el('article','booking-card');const info=el('div');info.append(el('span','status '+b.status,b.status),el('h3','',b.event_title),el('p','quiet',`${date(b.starts_at)} · Row ${String.fromCharCode(64+b.row)} / Seat ${b.number} · ${money(b.price_cents)}`));
  info.append(el('p','quiet','Booking #'+b.id));
  if(b.status==='pending'){const p=el('p','quiet');p.append(document.createTextNode('Hold expires in '));const t=el('span','timer');t.dataset.expires=b.expires_at;p.append(t);info.append(p);}
  const actions=el('div','booking-actions');if(b.status==='pending'){
   for(const [action,label,cls] of [['confirm','Confirm (demo) ↗','lime'],['cancel','Release seat','secondary']]){const button=el('button','button '+cls,label);button.type='button';button.onclick=()=>transition(b.id,action,actions);actions.append(button);}
  }else{const a=el('a','button secondary','View event ↗');a.href='#event/'+b.event_id;actions.append(a);}
  card.append(info,actions);return card;
 });$('bookings-list').replaceChildren(...(cards.length?cards:[el('div','empty','Your calendar has room for something good. Explore the lineup to book a seat.')]));tick();
}
async function transition(id,action,container){
 const gen=state.generation;for(const b of container.querySelectorAll('button'))b.disabled=true;
 try{await api(`/api/bookings/${id}/${action}`,{method:'POST'});toast(action==='confirm'?'You’re in. Your reservation is confirmed.':'Seat released.');}
 catch(error){toast(error.message);}
 finally{if(gen===state.generation){try{await loadBookings(false,gen);}catch(error){toast(error.message);for(const b of container.querySelectorAll('button'))b.disabled=false;}}}
}
$('refresh-bookings').onclick=()=>loadBookings(false).catch(e=>toast(e.message));$('more-bookings').onclick=async()=>{const b=$('more-bookings');b.disabled=true;try{await loadBookings(true);}catch(e){toast(e.message);}finally{b.disabled=false;}};
function tick(){for(const t of document.querySelectorAll('[data-expires]')){const secs=Math.max(0,Math.ceil((Date.parse(t.dataset.expires)-Date.now()-state.offset)/1000));t.textContent=`${Math.floor(secs/60)}:${String(secs%60).padStart(2,'0')}`;if(secs===0&&!t.dataset.done){t.dataset.done='yes';const card=t.closest('.booking-card');for(const b of card.querySelectorAll('button'))b.disabled=true;setTimeout(()=>{if(state.route==='#bookings')loadBookings(false).catch(e=>toast(e.message));},1000);}}}
function openAuth(){if(!$('auth-dialog').open)$('auth-dialog').showModal();}
$('auth-open').onclick=openAuth;$('auth-close').onclick=()=>$('auth-dialog').close();
$('auth-toggle').onclick=()=>{state.mode=state.mode==='login'?'register':'login';const register=state.mode==='register';$('auth-title').textContent=register?'A good night starts here.':'Good to have you here.';$('auth-subtitle').textContent=register?'Create your account and find your next event.':'Sign in to save your spot.';$('auth-submit').textContent=register?'Create account ↗':'Sign in ↗';$('auth-toggle').textContent=register?'Already a member? Sign in':'New here? Create an account';$('auth-form').elements.password.autocomplete=register?'new-password':'current-password';$('auth-error').textContent='';};
$('auth-form').onsubmit=async event=>{
 event.preventDefault();const form=event.currentTarget;const button=$('auth-submit');button.disabled=true;$('auth-error').textContent='';
 try{state.user=await api('/api/auth/'+state.mode,{method:'POST',body:JSON.stringify({email:form.elements.email.value,password:form.elements.password.value})});form.reset();$('auth-dialog').close();updateAccount();toast('Welcome. Let’s find you a seat.');if(state.route==='#bookings')await route();}
 catch(error){$('auth-error').textContent=error.message;}finally{button.disabled=false;}
};
$('logout').onclick=async()=>{try{await api('/api/auth/logout',{method:'POST'});state.user=null;state.request=null;updateAccount();if(location.hash==='#events')await route();else location.hash='#events';toast('Signed out.');}catch(error){toast(error.message);}};
$('event-form').onsubmit=async event=>{
 event.preventDefault();const form=event.currentTarget,fields=form.elements,button=form.querySelector('button[type=submit]');button.disabled=true;$('event-error').textContent='';
 try{const starts=new Date(fields.starts_at.value);if(!Number.isFinite(starts.getTime()))throw new Error('Choose a valid date.');const result=await api('/api/admin/events',{method:'POST',body:JSON.stringify({title:fields.title.value,description:fields.description.value,starts_at:starts.toISOString(),price_cents:Math.round(Number(fields.price.value)*100),rows:Number(fields.rows.value),columns:Number(fields.columns.value)})});form.reset();toast('Your event is live.');location.hash='#event/'+result.id;}
 catch(error){$('event-error').textContent=error.message;}finally{button.disabled=false;}
};
window.addEventListener('hashchange',route);setInterval(tick,1000);
setInterval(async()=>{if(document.hidden||state.busy||detailLoading)return;const gen=state.generation;
 if(state.route.startsWith('#event/')&&state.event){detailLoading=true;try{await loadDetail(state.event.id,gen);$('seat-update').textContent='Availability updated · refreshes every 5 seconds.';}catch(e){if(gen===state.generation)$('seat-update').textContent='Update failed. Check your connection; retrying shortly.';}finally{detailLoading=false;}}
},5000);
(async()=>{try{state.user=await api('/api/me');}catch(e){if(e.status!==401)toast('Connection failed. Please refresh the page.');}updateAccount();await route();})();
