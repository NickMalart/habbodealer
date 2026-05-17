<template>
  <div class="container">
    <h1>Auto Payout Bot</h1>

    <div class="payout-form">
      <input v-model="newName" placeholder="Habbo Name" />
      <input v-model="newItem" placeholder="Item Name" />
      <input v-model.number="newQty" type="number" placeholder="Qty" style="width: 60px;" />
      <button @click="addPayout">Add Payout</button>
    </div>

    <div class="actions">
      <button @click="clearCompleted" class="btn-secondary">Clear Completed</button>
      <button @click="refreshInventory" class="btn-secondary">Refresh Inventory</button>
      <button @click="returnToOwner" class="btn-danger">Return All to Owner</button>
    </div>

    <table style="margin-top: 1rem;">
      <thead>
        <tr>
          <th>Name</th>
          <th>Item</th>
          <th>Qty</th>
          <th>Status</th>
          <th>Created</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in payouts" :key="p.id">
          <td>{{ p.name }}</td>
          <td>{{ p.itemName }}</td>
          <td>{{ p.quantity }}</td>
          <td>
            <span :class="'status-' + p.status.toLowerCase().replace(' ', '-')">
              {{ p.status }}
            </span>
          </td>
          <td style="font-size: 0.8em; opacity: 0.6;">{{ p.createdAt }}</td>
          <td>
            <button @click="deletePayout(p.id)" class="btn-danger" style="padding: 2px 8px;">×</button>
          </td>
        </tr>
        <tr v-if="payouts.length === 0">
          <td colspan="6" style="text-align: center; opacity: 0.5;">No pending payouts</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'

const payouts = ref([])
const newName = ref('')
const newItem = ref('')
const newQty = ref(1)

const loadPayouts = async () => {
  payouts.value = await window.go.main.App.GetPayouts()
}

const addPayout = async () => {
  console.log('addPayout clicked', newName.value, newItem.value, newQty.value)
  if (!newName.value || !newItem.value) return
  
  if (!window.go || !window.go.main || !window.go.main.App) {
    console.error('Wails backend not found!')
    alert('Error: Bot backend not connected. Try restarting the app.')
    return
  }

  try {
    await window.go.main.App.AddPayout(newName.value, newItem.value, newQty.value)
    console.log('AddPayout successful')
    newName.value = ''
    newItem.value = ''
    newQty.value = 1
  } catch (err) {
    console.error('AddPayout failed:', err)
    alert('Error adding payout: ' + err)
  }
}

const deletePayout = async (id) => {
  await window.go.main.App.DeletePayout(id)
}

const clearCompleted = async () => {
  await window.go.main.App.ClearCompleted()
}

const refreshInventory = async () => {
  await window.go.main.App.RefreshInventory()
}

const returnToOwner = async () => {
  const name = prompt("Enter owner name to return all items to:")
  if (name) {
    await window.go.main.App.ReturnAllToOwner(name)
  }
}

onMounted(() => {
  loadPayouts()
  window.runtime.EventsOn('payoutsUpdate', (data) => {
    payouts.value = data
  })
})
</script>
