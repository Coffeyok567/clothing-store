const API = "";
const state = { role: "guest", userId: "", userName: "", login: "", page: 1, limit: 12, currentProduct: null, adminTab: "products", products: [], cart: [] };
const $ = (id) => document.getElementById(id);
const money = (n) => new Intl.NumberFormat("ru-RU",{style:"currency",currency:"RUB",maximumFractionDigits:0}).format(Number(n)||0);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const headers = (extra={}) => ({...extra});
async function api(path, options={}) {
  const res = await fetch(API+path,{credentials:"same-origin",...options,headers:headers(options.headers||{})});
  if(res.status===204) return null;
  const data = await res.json().catch(()=>({}));
  if(!res.ok) throw new Error(data.error || `Ошибка ${res.status}`);
  return data;
}
function toast(msg){const t=$("toast");t.textContent=msg;t.classList.add("show");setTimeout(()=>t.classList.remove("show"),2600)}
function page(name){
  if(name==="cart" && state.role!=="client"){toast("Корзина доступна клиенту");return;}
  if(name==="orders" && state.role==="guest"){toast("Сначала войдите в аккаунт");openAuthDialog("login");return;}
  if(name==="admin" && state.role!=="admin"){toast("Раздел доступен администратору");return;}
  document.querySelectorAll(".page").forEach(x=>x.classList.remove("active"));
  const target=$(`${name}-page`); if(target)target.classList.add("active");
  document.querySelectorAll(".nav-link").forEach(x=>x.classList.toggle("active",x.dataset.page===name));
  state.pageName=name;
  if(name==="catalog")loadProducts();
  if(name==="cart")loadCart();
  if(name==="orders")loadOrders();
  if(name==="admin")loadAdmin();
  window.scrollTo({top:0,behavior:"smooth"});
}
function updateIdentity(){
  $("user-label").textContent=state.role==="guest"?"Войти":(state.userName||({client:"Клиент",manager:"Менеджер",admin:"Администратор"}[state.role]||"Пользователь"));
  document.querySelectorAll("[data-role]").forEach(el=>{
    const allowed=el.dataset.role.split(",");
    el.style.display=allowed.includes(state.role)?"":"none";
  });
}
function getFilters(){
  const p=new URLSearchParams();
  [["type","f-type"],["gender","f-gender"],["size","f-size"],["color","f-color"],["brand","f-brand"],["season","f-season"],["sort","f-sort"]].forEach(([k,id])=>{const v=$(id).value.trim();if(v)p.set(k,v)});
  p.set("page",state.page);p.set("limit",state.limit);return p.toString();
}
async function loadProducts(){
  $("product-grid").innerHTML='<div class="loading">Загружаем каталог…</div>';
  try{
    const items=await api("/product?"+getFilters());state.products=items;
    $("catalog-total").textContent=items.length?`Показано: ${items.length}`:"";
    $("page-label").textContent=`Страница ${state.page}`;
    $("prev-page").disabled=state.page===1;
    $("product-grid").innerHTML=items.length?items.map(p=>`
      <article class="product-card" data-open-product="${esc(p.id)}">
        <div class="product-image">${p.photos?.length?`<img src="/photo/${encodeURIComponent(p.photos[0])}" alt="${esc(p.name)}" onerror="this.remove()">`:`<span class="image-placeholder">${esc((p.type||"F").slice(0,1).toUpperCase())}</span>`}</div>
        <div class="product-info"><div class="product-meta"><span>${esc(p.brand)}</span><span>${esc(p.season)}</span></div><h3>${esc(p.name)}</h3><div class="product-sub">${esc(p.type)} · ${esc(p.size)} · ${esc(p.color)}</div><div class="product-bottom"><span class="price">${money(p.price)}</span><button class="plus" data-add="${esc(p.id)}" title="Добавить">+</button></div></div>
      </article>`).join(""):'<div class="empty">Товары не найдены. Попробуйте изменить фильтры.</div>';
  }catch(e){$("product-grid").innerHTML=`<div class="empty">${esc(e.message)}<br><br>Проверьте, запущен ли сервер.</div>`}
}
async function openProduct(id){
  try{
    const p=await api("/product/"+encodeURIComponent(id));state.currentProduct=p;
    $("product-detail").innerHTML=`<div class="detail-layout"><div class="detail-image">${p.photos?.length?`<img src="/photo/${encodeURIComponent(p.photos[0])}" alt="${esc(p.name)}">`:`<span class="image-placeholder">${esc(p.type.slice(0,1).toUpperCase())}</span>`}</div><div class="detail-copy"><span class="eyebrow">${esc(p.brand)} · ${esc(p.season)}</span><h2>${esc(p.name)}</h2><p>${esc(p.description)}</p><div class="detail-price">${money(p.price)}</div><div class="detail-specs"><div class="spec"><small>Тип</small><b>${esc(p.type)}</b></div><div class="spec"><small>Пол</small><b>${p.gender==="м"?"Мужской":"Женский"}</b></div><div class="spec"><small>Размер</small><b>${esc(p.size)}</b></div><div class="spec"><small>Цвет</small><b>${esc(p.color)}</b></div></div>${state.role==="client"?`<button class="primary-btn" id="detail-add">Добавить в корзину　＋</button>`:`<p class="muted">Чтобы добавить товар в корзину, выберите профиль клиента.</p>`}</div></div>`;
    $("detail-add")?.addEventListener("click",()=>addCart(p.id));
    document.querySelectorAll(".page").forEach(x=>x.classList.remove("active"));$("product-page").classList.add("active");window.scrollTo(0,0);
  }catch(e){toast(e.message)}
}
async function addCart(id){try{await api("/cart",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({product_id:id,count:1})});toast("Товар добавлен в корзину");refreshCartCount()}catch(e){toast(e.message)}}
async function refreshCartCount(){if(state.role!=="client"||!state.userId){$("cart-count").textContent="0";return}try{const x=await api("/cart/"+encodeURIComponent(state.userId));$("cart-count").textContent=x.reduce((n,i)=>n+i.count,0)}catch{$("cart-count").textContent="0"}}
async function loadCart(){
  $("cart-content").innerHTML='<div class="loading">Загружаем корзину…</div>';
  try{
    const items=await api("/cart/"+encodeURIComponent(state.userId));state.cart=items;
    if(!items.length){$("cart-content").innerHTML='<div class="empty">В корзине пока пусто. Самое время выбрать что-нибудь в каталоге.</div>';refreshCartCount();return}
    const total=items.reduce((s,x)=>s+x.price*x.count,0);
    $("cart-content").innerHTML=`<div class="cart-list">${items.map(x=>`<div class="cart-row"><div><div class="cart-title">${esc(x.product_name)}</div><small>${money(x.price)} за шт.</small></div><div class="qty"><button data-qty="${esc(x.id)}" data-delta="-1">−</button><b>${x.count}</b><button data-qty="${esc(x.id)}" data-delta="1">+</button></div><b>${money(x.price*x.count)}</b><button class="small-btn danger" data-remove-cart="${esc(x.id)}">Удалить</button></div>`).join("")}</div><div class="cart-total"><span>Итого</span><strong>${money(total)}</strong><button class="primary-btn" id="checkout">Оформить заказ　→</button></div>`;
    refreshCartCount();
  }catch(e){$("cart-content").innerHTML=`<div class="empty">${esc(e.message)}</div>`}
}
async function loadOrders(){
  $("orders-content").innerHTML='<div class="loading">Загружаем заказы…</div>';
  try{
    const q=new URLSearchParams({page:"1",limit:"100"});if($("order-status").value)q.set("status",$("order-status").value);
    const orders=await api("/order?"+q.toString());
    if(!orders.length){$("orders-content").innerHTML='<div class="empty">Заказов пока нет.</div>';return}
    $("orders-content").innerHTML=`<div class="order-list">${orders.map(o=>`<article class="order-card"><div><b>Заказ #${esc(o.id.slice(0,8))}</b><small>${esc(o.user_name||"")} · ${new Date(o.date_time).toLocaleString("ru-RU")}</small></div><span class="status">${esc(o.status)}</span><button class="outline-btn" data-order-detail="${esc(o.id)}">Состав заказа</button>${["admin","manager"].includes(state.role)?`<select data-order-status="${esc(o.id)}" class="status-select">${["Принят","В работе","Собран","Готов к выдаче","Отменён"].map(s=>`<option ${s===o.status?"selected":""}>${s}</option>`).join("")}</select>`:""}</article>`).join("")}</div>`;
  }catch(e){$("orders-content").innerHTML=`<div class="empty">${esc(e.message)}</div>`}
}
async function showOrderItems(id){
  try{const items=await api("/order_item/"+encodeURIComponent(id));const text=items.map(i=>`${i.product_name} — ${i.count} шт. × ${money(i.price)}`).join("\n");alert(text||"В заказе нет позиций")}
  catch(e){toast(e.message)}
}
async function loadAdmin(){await loadAdminTab(state.adminTab)}
async function loadAdminTab(tab){
  state.adminTab=tab;document.querySelectorAll(".tab").forEach(b=>b.classList.toggle("active",b.dataset.adminTab===tab));
  const host=$("admin-content");host.innerHTML='<div class="loading">Загрузка…</div>';
  try{
    if(tab==="products"){
      const items=await api("/product?page=1&limit=100");
      host.innerHTML=`<div class="form-panel"><h3 id="product-form-title">Добавить товар</h3><form id="product-form"><input type="hidden" id="p-id"><div class="form-grid"><label>Название<input id="p-name" required></label><label>Тип<input id="p-type" required></label><label>Пол<select id="p-gender"><option value="м">Мужской</option><option value="ж">Женский</option></select></label><label>Размер<input id="p-size" required></label><label>Цвет<input id="p-color" required></label><label>Бренд<input id="p-brand" required></label><label>Сезон<input id="p-season" required></label><label>Цена, ₽<input id="p-price" type="number" min="1" step="1" required></label><label class="wide">Описание<textarea id="p-description" required></textarea></label></div><div class="form-actions"><button type="reset" class="outline-btn" id="clear-product-form">Очистить</button><button class="primary-btn">Сохранить товар</button></div></form></div><div class="admin-toolbar"><b>Товары (${items.length})</b></div><div class="table-wrap"><table><thead><tr><th>Название</th><th>Тип</th><th>Размер</th><th>Бренд</th><th>Цена</th><th>Фото</th><th>Действия</th></tr></thead><tbody>${items.map(p=>`<tr><td>${esc(p.name)}</td><td>${esc(p.type)}</td><td>${esc(p.size)}</td><td>${esc(p.brand)}</td><td>${money(p.price)}</td><td><form data-photo-form="${esc(p.id)}"><input type="file" name="file" accept="image/jpeg,image/png,image/webp" required style="max-width:145px;font-size:10px"><button class="small-btn">Загрузить</button></form></td><td><div class="table-actions"><button class="small-btn" data-edit-product="${esc(p.id)}">Изменить</button><button class="small-btn danger" data-delete-product="${esc(p.id)}">Удалить</button></div></td></tr>`).join("")}</tbody></table></div>`;
      $("product-form").addEventListener("submit",saveProduct);
      $("clear-product-form").addEventListener("click",()=>setTimeout(()=>{$("p-id").value="";$("product-form-title").textContent="Добавить товар"},0));
    }else if(tab==="users"){
      const [users,roles]=await Promise.all([api("/user?page=1&limit=100"),api("/role")]);
      host.innerHTML=`<div class="form-panel"><h3>Создать пользователя</h3><form id="user-form"><div class="form-grid"><label>Имя<input id="u-name" required></label><label>Email<input id="u-email" type="email" required></label><label>Телефон<input id="u-phone" required maxlength="12"></label><label>Логин<input id="u-login" required minlength="3" maxlength="32" pattern="[A-Za-z0-9._-]+"></label><label>Пароль<input id="u-password" type="password" required minlength="8" maxlength="72"></label><label>Роль<select id="u-role"><option value="">Клиент по умолчанию</option>${roles.map(r=>`<option value="${esc(r.id)}">${esc(r.name)}</option>`).join("")}</select></label></div><div class="form-actions"><button class="primary-btn">Создать пользователя</button></div></form><div class="hint">Логин и пароль передайте созданному пользователю. Роль назначается администратором.</div></div><div class="table-wrap"><table><thead><tr><th>Имя</th><th>Логин</th><th>Email</th><th>Телефон</th><th>Роль</th><th>ID</th><th></th></tr></thead><tbody>${users.map(u=>`<tr><td>${esc(u.name)}</td><td>${esc(u.login)}</td><td>${esc(u.email)}</td><td>${esc(u.phone)}</td><td>${esc(u.role)}</td><td><code>${esc(u.id)}</code></td><td><button class="small-btn" data-copy="${esc(u.id)}">Копировать ID</button></td></tr>`).join("")}</tbody></table></div>`;
      $("user-form").addEventListener("submit",createUser);
    }else if(tab==="roles"){
      const roles=await api("/role");
      host.innerHTML=`<div class="form-panel"><h3>Добавить роль</h3><form id="role-form" class="toolbar"><input id="role-name" placeholder="Название роли" required><button class="primary-btn">Создать</button></form></div><div class="table-wrap"><table><thead><tr><th>Название</th><th>ID</th><th></th></tr></thead><tbody>${roles.map(r=>`<tr><td>${esc(r.name)}</td><td><code>${esc(r.id)}</code></td><td><button class="small-btn danger" data-delete-role="${esc(r.id)}">Удалить</button></td></tr>`).join("")}</tbody></table></div>`;
      $("role-form").addEventListener("submit",createRole);
    }else if(tab==="orders"){
      const orders=await api("/order?page=1&limit=100");
      host.innerHTML=`<div class="order-list">${orders.map(o=>`<article class="order-card"><div><b>Заказ #${esc(o.id.slice(0,8))}</b><small>${esc(o.user_name)} · ${new Date(o.date_time).toLocaleString("ru-RU")}</small></div><span class="status">${esc(o.status)}</span><button class="outline-btn" data-order-detail="${esc(o.id)}">Состав</button><select data-order-status="${esc(o.id)}" class="status-select">${["Принят","В работе","Собран","Готов к выдаче","Отменён"].map(s=>`<option ${s===o.status?"selected":""}>${s}</option>`).join("")}</select></article>`).join("")||'<div class="empty">Заказов пока нет.</div>'}</div>`;
    }
  }catch(e){host.innerHTML=`<div class="empty">${esc(e.message)}<br><br>Убедитесь, что выбран администратор с корректным ID.</div>`}
}
async function saveProduct(e){
  e.preventDefault();const id=$("p-id").value;
  const body={name:$("p-name").value,type:$("p-type").value,gender:$("p-gender").value,size:$("p-size").value,color:$("p-color").value,brand:$("p-brand").value,season:$("p-season").value,price:Number($("p-price").value),description:$("p-description").value};
  try{await api(id?"/product/"+id:"/product",{method:id?"PUT":"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});toast("Товар сохранён");loadAdminTab("products")}
  catch(err){toast(err.message)}
}
async function createUser(e){
  e.preventDefault();const body={name:$("u-name").value,email:$("u-email").value,phone:$("u-phone").value,role:$("u-role").value,login:$("u-login").value,password:$("u-password").value};
  try{const u=await api("/user",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});await navigator.clipboard?.writeText(u.id).catch(()=>{});alert(`Пользователь создан.\nID: ${u.id}\nID скопирован, если браузер разрешил.`);loadAdminTab("users")}
  catch(err){toast(err.message)}
}
async function createRole(e){e.preventDefault();try{await api("/role",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({name:$("role-name").value})});toast("Роль создана");loadAdminTab("roles")}catch(err){toast(err.message)}}
document.addEventListener("click",async e=>{
  const nav=e.target.closest("[data-page]");if(nav){e.preventDefault();page(nav.dataset.page);return}
  const add=e.target.closest("[data-add]");if(add){e.stopPropagation();addCart(add.dataset.add);return}
  const card=e.target.closest("[data-open-product]");if(card){openProduct(card.dataset.openProduct);return}
  const edit=e.target.closest("[data-edit-product]");if(edit){const p=await api("/product/"+edit.dataset.editProduct).catch(x=>{toast(x.message);return null});if(!p)return;$("p-id").value=p.id;["name","type","gender","size","color","brand","season","price","description"].forEach(k=>$("p-"+k).value=p[k]);$("product-form-title").textContent="Изменить товар";window.scrollTo({top:500,behavior:"smooth"});return}
  const del=e.target.closest("[data-delete-product]");if(del&&confirm("Удалить товар?")){try{await api("/product/"+del.dataset.deleteProduct,{method:"DELETE"});toast("Товар удалён");loadAdminTab("products")}catch(x){toast(x.message)}return}
  const qty=e.target.closest("[data-qty]");if(qty){const item=state.cart.find(x=>x.id===qty.dataset.qty);const n=item.count+Number(qty.dataset.delta);try{if(n<1)await api("/cart/"+item.id,{method:"DELETE"});else await api("/cart/"+item.id,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({count:n})});loadCart()}catch(x){toast(x.message)}return}
  const rem=e.target.closest("[data-remove-cart]");if(rem){try{await api("/cart/"+rem.dataset.removeCart,{method:"DELETE"});loadCart()}catch(x){toast(x.message)}return}
  const detail=e.target.closest("[data-order-detail]");if(detail){showOrderItems(detail.dataset.orderDetail);return}
  const copy=e.target.closest("[data-copy]");if(copy){navigator.clipboard?.writeText(copy.dataset.copy).then(()=>toast("ID скопирован")).catch(()=>toast(copy.dataset.copy));return}
  const roleDel=e.target.closest("[data-delete-role]");if(roleDel&&confirm("Удалить роль?")){try{await api("/role/"+roleDel.dataset.deleteRole,{method:"DELETE"});loadAdminTab("roles")}catch(x){toast(x.message)}return}
  const tab=e.target.closest("[data-admin-tab]");if(tab){loadAdminTab(tab.dataset.adminTab);return}
  if(e.target.id==="checkout"){try{const o=await api("/order",{method:"POST"});toast("Заказ оформлен");page("orders")}catch(x){toast(x.message)}}
});
document.addEventListener("change",async e=>{
  const photo=e.target.closest("[data-photo-form]");if(photo){e.preventDefault();const fd=new FormData(photo);try{await api("/photo",{method:"POST",body:fd});toast("Фотография загружена")}catch(x){toast(x.message)}return}
  const status=e.target.closest("[data-order-status]");if(status){try{await api("/order/"+status.dataset.orderStatus,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({status:status.value})});toast("Статус обновлён")}catch(x){toast(x.message)}}
});
$("apply-filters").addEventListener("click",()=>{state.page=1;loadProducts()});
$("reset-filters").addEventListener("click",()=>{["f-type","f-gender","f-size","f-color","f-brand","f-season","f-sort"].forEach(id=>$(id).value="");state.page=1;loadProducts()});
$("prev-page").addEventListener("click",()=>{if(state.page>1){state.page--;loadProducts()}});
$("next-page").addEventListener("click",()=>{state.page++;loadProducts()});
function openAuthDialog(panel="login"){
  const loggedIn=state.role!=="guest";
  $("auth-current").classList.toggle("hidden",!loggedIn);
  $("auth-forms").classList.toggle("hidden",loggedIn);
  $("auth-login-panel").classList.toggle("hidden",panel!=="login");
  $("auth-register-panel").classList.toggle("hidden",panel!=="register");
  $("auth-current-name").textContent=loggedIn?`${state.userName} · ${state.login}`:"";
  $("user-dialog").showModal();
}
$("user-trigger").addEventListener("click",()=>openAuthDialog("login"));
$("show-register").addEventListener("click",()=>openAuthDialog("register"));
$("show-login").addEventListener("click",()=>openAuthDialog("login"));
$("login-form").addEventListener("submit",async e=>{
  e.preventDefault();
  try{const user=await api("/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({login:$("login-name").value,password:$("login-password").value})});
    setAuthenticatedUser(user);$("login-form").reset();$("user-dialog").close();toast("Вы успешно вошли");page("catalog");
  }catch(err){toast(err.message)}
});
$("register-form").addEventListener("submit",async e=>{
  e.preventDefault();
  try{const user=await api("/auth/register",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({name:$("reg-name").value,email:$("reg-email").value,phone:$("reg-phone").value,login:$("reg-login").value,password:$("reg-password").value})});
    setAuthenticatedUser(user);$("register-form").reset();$("user-dialog").close();toast("Аккаунт создан");page("catalog");
  }catch(err){toast(err.message)}
});
function setAuthenticatedUser(user){state.userId=user.id;state.userName=user.name;state.login=user.login;state.role=user.role;updateIdentity();refreshCartCount()}
$("logout-btn").addEventListener("click",async()=>{try{await api("/auth/logout",{method:"POST"})}catch{}state.role="guest";state.userId="";state.userName="";state.login="";$("user-dialog").close();updateIdentity();refreshCartCount();page("catalog");toast("Вы вышли из аккаунта")});
$("refresh-orders").addEventListener("click",loadOrders);$("order-status").addEventListener("change",loadOrders);
async function initializeAuth(){
  try{const user=await api("/auth/me");setAuthenticatedUser(user)}catch{state.role="guest";state.userId="";state.userName="";state.login="";updateIdentity()}
  loadProducts();refreshCartCount();
}
updateIdentity();initializeAuth();
