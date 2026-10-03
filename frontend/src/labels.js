export const STATUS_LABEL = {
  packed: '已装箱', in_transit: '在途', delivered: '已送达', signed: '已签收', closed: '已结清',
}
export const STATUS_CLS = {
  packed: 'info', in_transit: 'warn', delivered: 'warn', signed: 'ok', closed: 'gray',
}
export const MEAL_LABEL = { breakfast: '早餐', lunch: '午餐', dinner: '晚餐' }
export const ROLE_LABEL = {
  kitchen: '厨房', courier: '配送员', frontdesk: '企业前台', reviewer: '复核员',
}
export const DISC_LABEL = {
  open: '待复核', approved: '已批准待处理', rejected: '已驳回', processed: '已处理',
}
export const DISC_CLS = { open: 'warn', approved: 'info', rejected: 'bad', processed: 'ok' }
export const KIND_LABEL = {
  shortage: '少盒', wrong: '错餐', damage: '封签破损', nobody: '无人签收',
}
export const RES_LABEL = { resupply: '补送', loss: '损失核销', refund: '退款' }
