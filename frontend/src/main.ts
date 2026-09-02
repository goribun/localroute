import {createApp} from 'vue';
import App from './App.vue';
import './style.css';

function report(kind:string,value:unknown){
  void fetch('/api/diagnostic',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({kind,value:String(value),stack:value instanceof Error?value.stack:''})});
}
window.addEventListener('error',event=>report('window.error',event.error||event.message));
window.addEventListener('unhandledrejection',event=>report('unhandledrejection',event.reason));
const app=createApp(App);
app.config.errorHandler=(error,_,info)=>report(`vue:${info}`,error);
app.mount('#app');
