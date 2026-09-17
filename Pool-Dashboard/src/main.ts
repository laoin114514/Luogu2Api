import { createApp } from 'vue'
import { vLoading } from 'element-plus'
import 'element-plus/es/components/loading/style/css'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

import App from './App.vue'
import './styles.css'

createApp(App)
  .directive('loading', vLoading)
  .mount('#app')
