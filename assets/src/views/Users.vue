<template>
  <div class="dashboard">
    <ul id="nav">
      <li>
        <router-link to="/settings/global">
          <i class="material-icons">keyboard_arrow_left</i> {{ $t('settings.globalSettings') }}
        </router-link>
      </li>
      <li></li>
    </ul>

    <h1>{{ $t('settings.users') }} <router-link to="/users/new"><button>{{ $t('buttons.new') }}</button></router-link></h1>

    <table>
      <tr>
        <th>{{ $t('settings.username') }}</th>
        <th>{{ $t('settings.admin') }}</th>
        <th>{{ $t('settings.scope') }}</th>
        <th></th>
      </tr>

      <tr v-for="user in users">
        <td>{{ user.username }}</td>
        <td><i v-if="user.admin" class="material-icons">done</i><i v-else class="material-icons">close</i></td>
        <td>{{ user.filesystem }}</td>
        <td><router-link :to="'/users/' + user.ID"><i class="material-icons">mode_edit</i></router-link></td>
      </tr>

    </table>
  </div>
</template>

<script>
import * as api from '@/utils/api'

export default {
  name: 'users',
  data: function () {
    return {
      users: []
    }
  },
  created () {
    api.getUsers().then(users => {
      this.users = users
    }).catch(error => {
      this.$store.commit('showError', error)
    })
  }
}
</script>
